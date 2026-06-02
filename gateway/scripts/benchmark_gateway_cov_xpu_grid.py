#!/usr/bin/env python3
"""Benchmark gateway cov predict_for_best runtimes for a context/horizon grid."""

from __future__ import annotations

import argparse
import json
import time
import urllib.request
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo


def _request_json(method: str, url: str, payload: dict | None, timeout: int) -> tuple[int, dict, float]:
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    started = time.time()
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        raw = resp.read().decode("utf-8")
        elapsed = time.time() - started
        return resp.status, json.loads(raw), elapsed


def _parse_int_list(value: str) -> list[int]:
    return [int(part.strip()) for part in value.split(",") if part.strip()]


def _nested_get(value: object, path: tuple[str, ...]) -> object:
    current = value
    for key in path:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def _poll_job(gateway_url: str, job_id: str, timeout: int, interval: float) -> tuple[dict, float, list[dict]]:
    deadline = time.time() + timeout
    started = time.time()
    history = []
    last = {}
    while time.time() < deadline:
        _, status, http_elapsed_sec = _request_json(
            "GET",
            f"{gateway_url.rstrip('/')}/jobs/{job_id}",
            None,
            min(timeout, 120),
        )
        last = status
        history.append(
            {
                "at_sec": time.time() - started,
                "http_elapsed_sec": http_elapsed_sec,
                "status": status.get("status"),
                "current_stage": status.get("current_stage"),
                "backend": status.get("backend"),
            }
        )
        if status.get("status") in {"succeeded", "failed"}:
            return status, time.time() - started, history
        time.sleep(interval)
    raise TimeoutError(f"job {job_id} did not finish within {timeout}s; last={last}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--gateway-url", default="http://127.0.0.1:59010")
    parser.add_argument("--stock-code", default="510050")
    parser.add_argument("--stock-type", type=int, default=2)
    parser.add_argument("--years", type=int, default=15)
    parser.add_argument("--context-lens", default="512,1024,2048")
    parser.add_argument("--horizon-lens", default="7,14,28")
    parser.add_argument("--timeout", type=int, default=7200)
    parser.add_argument("--poll-interval", type=float, default=2.0)
    parser.add_argument(
        "--output",
        default="forecast-results/xpu_cov_inference_time_benchmark_510050_20260520.json",
    )
    args = parser.parse_args()

    gateway_url = args.gateway_url.rstrip("/")
    context_lens = _parse_int_list(args.context_lens)
    horizon_lens = _parse_int_list(args.horizon_lens)
    requested_at = datetime.now(ZoneInfo("Asia/Shanghai")).isoformat()

    results = []
    for context_len in context_lens:
        for horizon_len in horizon_lens:
            payload = {
                "stock_code": args.stock_code,
                "stock_type": args.stock_type,
                "years": args.years,
                "context_len": context_len,
                "horizon_len": horizon_len,
                "timesfm_version": "2.5",
                "covariate_config": {"enabled": True},
                "covariate_preset": "market_cov_v1",
                "force_enqueue": True,
                "user_id": 1,
            }
            print(f"benchmark start context_len={context_len} horizon_len={horizon_len}", flush=True)
            enqueue_status, enqueue_response, enqueue_http_elapsed_sec = _request_json(
                "POST",
                f"{gateway_url}/predict_for_best",
                payload,
                120,
            )
            job_id = enqueue_response.get("job_id")
            if not job_id:
                raise RuntimeError(f"missing job_id: {enqueue_response}")

            job_status, total_elapsed_sec, poll_history = _poll_job(
                gateway_url,
                job_id,
                args.timeout,
                args.poll_interval,
            )
            result_body = job_status.get("result") if isinstance(job_status, dict) else {}
            result_data = result_body.get("data") if isinstance(result_body, dict) else {}
            overall = result_data.get("overall_metrics") if isinstance(result_data, dict) else {}
            validation = overall.get("validation_results") if isinstance(overall, dict) else {}
            result = {
                "context_len": context_len,
                "horizon_len": horizon_len,
                "enqueue_status_code": enqueue_status,
                "job_id": job_id,
                "status": job_status.get("status"),
                "success": bool(result_body.get("success")) if isinstance(result_body, dict) else False,
                "gateway_url": gateway_url,
                "backend": job_status.get("backend"),
                "prediction_type": job_status.get("prediction_type"),
                "current_stage": job_status.get("current_stage"),
                "enqueue_http_elapsed_sec": enqueue_http_elapsed_sec,
                "total_elapsed_sec": total_elapsed_sec,
                "processing_time_sec": result_data.get("processing_time") if isinstance(result_data, dict) else None,
                "main_stage_processing_time_sec": _nested_get(result_body, ("stage_timings", "main", "processing_time")),
                "xreg_stage_processing_time_sec": _nested_get(result_body, ("stage_timings", "xreg", "processing_time")),
                "total_chunks": result_data.get("total_chunks") if isinstance(result_data, dict) else None,
                "validation_chunks": validation.get("validation_chunks") if isinstance(validation, dict) else None,
                "best_prediction_item": overall.get("best_prediction_item") if isinstance(overall, dict) else None,
                "best_metrics": overall.get("best_metrics") if isinstance(overall, dict) else None,
                "validation_results": validation if isinstance(validation, dict) else None,
                "poll_history": poll_history,
                "error": job_status.get("error")
                or (result_body.get("error") if isinstance(result_body, dict) else None),
            }
            results.append(result)
            print(
                "benchmark done "
                f"context_len={context_len} horizon_len={horizon_len} "
                f"status={result['status']} backend={result['backend']} "
                f"total_elapsed_sec={total_elapsed_sec:.3f} "
                f"processing_time_sec={result['processing_time_sec']}",
                flush=True,
            )

            output = {
                "generated_at": requested_at,
                "stock_code": args.stock_code,
                "stock_type": args.stock_type,
                "years": args.years,
                "prediction_type": "cov",
                "backend": "xpu",
                "gateway_url": gateway_url,
                "time_unit": "seconds",
                "estimate_field": "total_elapsed_sec",
                "results": results,
            }
            output_path = Path(args.output)
            output_path.parent.mkdir(parents=True, exist_ok=True)
            output_path.write_text(json.dumps(output, ensure_ascii=False, indent=2), encoding="utf-8")

    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
