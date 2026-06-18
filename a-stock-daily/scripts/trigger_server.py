#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import tempfile
import threading
import time
from datetime import datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlencode, urlparse
import urllib.request
from zoneinfo import ZoneInfo


RUN_LOCK = threading.Lock()


def env(name: str, default: str) -> str:
    return os.environ.get(name, default).strip() or default


def today_ymd() -> str:
    tz_name = env("TZ", "Asia/Shanghai")
    try:
        tz = ZoneInfo(tz_name)
    except Exception:
        tz = ZoneInfo("Asia/Shanghai")
    return datetime.now(tz).strftime("%Y%m%d")


def normalize_date(value: str) -> str:
    compact = value.replace("-", "").strip()
    if len(compact) != 8 or not compact.isdigit():
        raise ValueError(f"invalid date: {value}")
    return compact


def output_tail(file_obj: Any, limit: int = 20000) -> str:
    file_obj.seek(0, os.SEEK_END)
    size = file_obj.tell()
    file_obj.seek(max(0, size - limit), os.SEEK_SET)
    return file_obj.read()


class TriggerHandler(BaseHTTPRequestHandler):
    server_version = "AStockLevel1Trigger/1.0"

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/health":
            self.write_json(200, {"ok": True})
            return
        if parsed.path in {"/history", "/api/v1/history"}:
            if not self.authorized():
                self.write_json(401, {"error": "unauthorized"})
                return
            try:
                payload = history_payload_from_query(parse_qs(parsed.query))
                self.write_json(200, query_history(payload))
            except Exception as exc:
                self.write_json(400, {"error": str(exc)})
            return
        self.write_json(404, {"error": "not found"})

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path in {"/history", "/api/v1/history"}:
            if not self.authorized():
                self.write_json(401, {"error": "unauthorized"})
                return
            try:
                self.write_json(200, query_history(self.read_json_body()))
            except Exception as exc:
                self.write_json(400, {"error": str(exc)})
            return

        if parsed.path not in {"/daily", "/export"}:
            self.write_json(404, {"error": "not found"})
            return
        if not self.authorized():
            self.write_json(401, {"error": "unauthorized"})
            return

        try:
            query = parse_qs(parsed.query)
            body = self.read_json_body()
            date = normalize_date(str(body.get("date") or first_query(query, "date") or today_ymd()))
            concurrent = str(body.get("concurrent") or first_query(query, "concurrent") or env("DOWNLOAD_CONCURRENT", "50"))
            if not concurrent.isdigit() or int(concurrent) <= 0:
                raise ValueError(f"invalid concurrent: {concurrent}")
            mode = "daily" if parsed.path == "/daily" else "export"
            force = parse_bool(body.get("force"), truthy(first_query(query, "force")))
        except (ValueError, json.JSONDecodeError) as exc:
            self.write_json(400, {"error": str(exc)})
            return

        if not RUN_LOCK.acquire(blocking=False):
            self.write_json(409, {"error": "a level1 sync is already running"})
            return

        try:
            result = run_once(mode, date, concurrent, force)
        finally:
            RUN_LOCK.release()

        status = 200 if result["exit_code"] == 0 else 502
        self.write_json(status, result)

    def read_json_body(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length", "0") or "0")
        if length <= 0:
            return {}
        raw = self.rfile.read(length)
        if not raw:
            return {}
        return json.loads(raw.decode("utf-8"))

    def authorized(self) -> bool:
        token = os.environ.get("TRIGGER_TOKEN", "").strip()
        if not token:
            return True
        header_token = self.headers.get("X-Token", "").strip()
        auth = self.headers.get("Authorization", "").strip()
        bearer = auth.removeprefix("Bearer ").strip() if auth.startswith("Bearer ") else ""
        return header_token == token or bearer == token

    def write_json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args: Any) -> None:
        print(f"{self.address_string()} - {fmt % args}", flush=True)


def first_query(query: dict[str, list[str]], key: str) -> str:
    values = query.get(key) or []
    return values[0] if values else ""


def history_payload_from_query(query: dict[str, list[str]]) -> dict[str, Any]:
    return {
        "symbol": first_query(query, "symbol"),
        "stock_type": first_query(query, "stock_type") or first_query(query, "type") or "1",
        "start_date": first_query(query, "start_date"),
        "end_date": first_query(query, "end_date"),
        "adjust": first_query(query, "adjust"),
    }


def truthy(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


def parse_bool(value: Any, default: bool) -> bool:
    if value is None or value == "":
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return value != 0
    text = str(value).strip().lower()
    if text in {"1", "true", "yes", "on"}:
        return True
    if text in {"0", "false", "no", "off"}:
        return False
    return default


def clickhouse_endpoint() -> str:
    base = env("CLICKHOUSE_URL", "http://a-stock-clickhouse:8123")
    params = urlencode(
        {
            "user": env("CLICKHOUSE_USER", "stock_user"),
            "password": env("CLICKHOUSE_PASSWORD", "stock_pass"),
            "database": env("CLICKHOUSE_DATABASE", "stock_db"),
        }
    )
    return f"{base}/?{params}"


def clickhouse_query_json_each_row(sql: str) -> list[dict[str, Any]]:
    request = urllib.request.Request(
        clickhouse_endpoint(),
        data=sql.encode("utf-8"),
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=int(env("CLICKHOUSE_TIMEOUT", "60"))) as response:
        text = response.read().decode("utf-8")
    return [json.loads(line) for line in text.splitlines() if line.strip()]


def sql_string(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def normalize_symbol(value: Any) -> str:
    raw = str(value or "").strip()
    lower = raw.lower()
    for prefix in ("sh", "sz", "bj"):
        if lower.startswith(prefix) and len(raw) > len(prefix):
            return raw[len(prefix) :]
    return raw


def optional_date(value: Any) -> str:
    raw = str(value or "").strip()
    if not raw:
        return ""
    return normalize_date(raw)


def dashed_date(compact: str) -> str:
    return f"{compact[0:4]}-{compact[4:6]}-{compact[6:8]}"


def query_history(payload: dict[str, Any]) -> dict[str, Any]:
    symbol = normalize_symbol(payload.get("symbol"))
    if not symbol:
        raise ValueError("symbol is required")
    stock_type = int(payload.get("stock_type") or payload.get("type") or 1)
    start_date = optional_date(payload.get("start_date"))
    end_date = optional_date(payload.get("end_date"))
    adjust = normalize_adjust(payload.get("adjust"))

    filters = [f"code = {sql_string(symbol)}", f"adjust = {sql_string(adjust)}"]
    if start_date:
        filters.append(f"trade_date >= toDate({sql_string(dashed_date(start_date))})")
    if end_date:
        filters.append(f"trade_date <= toDate({sql_string(dashed_date(end_date))})")
    where_sql = " AND ".join(filters)

    rows = clickhouse_query_json_each_row(
        f"""
SELECT
    toString(trade_date) AS date_str,
    datetime,
    open,
    close,
    high,
    low,
    volume,
    amount,
    0.0 AS amplitude,
    0.0 AS percentage_change,
    0.0 AS amount_change,
    0.0 AS turnover_rate
FROM tdx_daily_bars FINAL
WHERE {where_sql} AND open > 0 AND close > 0
ORDER BY trade_date ASC
FORMAT JSONEachRow
"""
    )
    for row in rows:
        row["volume"] = int(row.get("volume") or 0)
    return {
        "code": 200,
        "symbol": symbol,
        "stock_type": stock_type,
        "provider": "a-stock-daily",
        "adjust": adjust,
        "rows": len(rows),
        "data": rows,
    }


def normalize_adjust(value: Any) -> str:
    raw = str(value or env("DEFAULT_HISTORY_ADJUST", "qfq")).strip().lower()
    mapping = {
        "": "qfq",
        "forward": "qfq",
        "forward_additive": "qfq",
        "qfq": "qfq",
        "backward": "hfq",
        "backward_additive": "hfq",
        "hfq": "hfq",
        "none": "none",
        "raw": "none",
    }
    if raw not in mapping:
        raise ValueError(f"unsupported adjust: {raw}; expected qfq|hfq|none")
    return mapping[raw]


def run_once(mode: str, date: str, concurrent: str, force: bool) -> dict[str, Any]:
    timeout = int(env("TRIGGER_TIMEOUT_SECONDS", "7200"))
    command = ["/app/scripts/run_once.sh", mode, date]
    if mode == "daily":
        command.append(concurrent)

    child_env = os.environ.copy()
    if force:
        child_env["FORCE_DOWNLOAD"] = "true"

    started_at = datetime.now().isoformat(timespec="seconds")
    with tempfile.TemporaryFile(mode="w+t", encoding="utf-8", errors="replace") as output:
        try:
            completed = subprocess.run(
                command,
                cwd="/app",
                env=child_env,
                stdout=output,
                stderr=subprocess.STDOUT,
                text=True,
                timeout=timeout,
                check=False,
            )
            exit_code = completed.returncode
        except subprocess.TimeoutExpired:
            return {
                "date": date,
                "mode": mode,
                "exit_code": 124,
                "started_at": started_at,
                "finished_at": datetime.now().isoformat(timespec="seconds"),
                "output": f"sync timed out after {timeout}s",
            }

        return {
            "date": date,
            "mode": mode,
            "exit_code": exit_code,
            "started_at": started_at,
            "finished_at": datetime.now().isoformat(timespec="seconds"),
            "output": output_tail(output),
        }


def seconds_until_next_run(hour: int, minute: int) -> int:
    tz_name = env("TZ", "Asia/Shanghai")
    try:
        tz = ZoneInfo(tz_name)
    except Exception:
        tz = ZoneInfo("Asia/Shanghai")
    now = datetime.now(tz)
    target = now.replace(hour=hour, minute=minute, second=0, microsecond=0)
    if target <= now:
        target = target + timedelta(days=1)
    return max(1, int((target - now).total_seconds()))


def auto_daily_loop() -> None:
    hour = int(env("DAILY_RUN_HOUR", "22"))
    minute = int(env("DAILY_RUN_MINUTE", "0"))
    concurrent = env("DOWNLOAD_CONCURRENT", "50")
    while True:
        wait_seconds = seconds_until_next_run(hour, minute)
        print(f"next automatic daily run in {wait_seconds}s at {hour:02d}:{minute:02d}", flush=True)
        time.sleep(wait_seconds)
        date = today_ymd()
        if not RUN_LOCK.acquire(blocking=False):
            print("automatic daily run skipped: another sync is running", flush=True)
            continue
        try:
            if env("DAILY_SYNC_MODE", "level1") == "tdx":
                command = ["/app/scripts/run_once.sh", "tdx-daily-watchlist"]
                completed = subprocess.run(command, cwd="/app", check=False)
                exit_code = completed.returncode
            else:
                result = run_once("daily", date, concurrent, False)
                exit_code = result["exit_code"]
            print(f"automatic daily run finished: date={date} exit_code={exit_code}", flush=True)
        finally:
            RUN_LOCK.release()


def main() -> None:
    port = int(env("SERVICE_PORT", "8080"))
    if truthy(env("AUTO_DAILY_SYNC_ENABLED", "true")):
        threading.Thread(target=auto_daily_loop, name="auto-daily-sync", daemon=True).start()
    server = ThreadingHTTPServer(("0.0.0.0", port), TriggerHandler)
    print(f"a-stock trigger server listening on :{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
