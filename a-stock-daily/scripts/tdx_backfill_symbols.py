#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime
from zoneinfo import ZoneInfo


def env(name: str, default: str) -> str:
    return os.environ.get(name, default).strip() or default


def clickhouse_endpoint() -> str:
    params = urllib.parse.urlencode(
        {
            "user": env("CLICKHOUSE_USER", "stock_user"),
            "password": env("CLICKHOUSE_PASSWORD", "stock_pass"),
            "database": env("CLICKHOUSE_DATABASE", "stock_db"),
        }
    )
    return f"{env('CLICKHOUSE_URL', 'http://a-stock-clickhouse:8123')}/?{params}"


def clickhouse_query(sql: str) -> str:
    request = urllib.request.Request(clickhouse_endpoint(), data=sql.encode("utf-8"), method="POST")
    with urllib.request.urlopen(request, timeout=int(env("CLICKHOUSE_TIMEOUT", "60"))) as response:
        return response.read().decode("utf-8")


def load_symbols() -> list[str]:
    text = clickhouse_query("SELECT code FROM stock_info ORDER BY code FORMAT JSONEachRow")
    symbols = [json.loads(line)["code"] for line in text.splitlines() if line.strip()]
    return [code for code in symbols if len(code) == 6 and code.isdigit() and code[0] in {"0", "1", "3", "5", "6"}]


def today_ymd() -> str:
    tz = ZoneInfo(env("TZ", "Asia/Shanghai"))
    return datetime.now(tz).strftime("%Y%m%d")


def normalize_date(raw: str) -> str:
    value = raw.replace("-", "").strip()
    if len(value) != 8 or not value.isdigit():
        raise ValueError(f"invalid date: {raw}")
    return value


def run_symbol(symbol: str, start: int, pages: int, count: int) -> tuple[str, int, str]:
    command = ["tdx_daily_bars", symbol, str(start), str(pages), str(count), "none"]
    child_env = os.environ.copy()
    child_env["TDX_INSERT_CLICKHOUSE"] = "true"
    completed = subprocess.run(
        command,
        cwd="/app",
        env=child_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        timeout=int(env("TDX_SYMBOL_TIMEOUT_SECONDS", "120")),
        check=False,
    )
    return symbol, completed.returncode, completed.stdout[-4000:]


def run_batch(symbols: list[str], start: int, pages: int, count: int, workers: int) -> tuple[int, list[tuple[str, str]]]:
    ok = 0
    failed: list[tuple[str, str]] = []
    started = time.time()

    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(run_symbol, symbol, start, pages, count) for symbol in symbols]
        for index, future in enumerate(as_completed(futures), start=1):
            symbol, code, output = future.result()
            if code == 0:
                ok += 1
            else:
                failed.append((symbol, output))
                print(f"failed {symbol}: {output}", file=sys.stderr, flush=True)

            if index % 50 == 0 or index == len(symbols):
                elapsed = max(0.1, time.time() - started)
                print(f"progress {index}/{len(symbols)} ok={ok} failed={len(failed)} speed={index / elapsed:.2f}/s", flush=True)

    return ok, failed


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "history"
    start = int(sys.argv[2]) if len(sys.argv) > 2 else 0
    pages = int(sys.argv[3]) if len(sys.argv) > 3 else (1 if mode == "daily" else int(env("TDX_BACKFILL_PAGES", "8")))
    count = int(sys.argv[4]) if len(sys.argv) > 4 else int(env("TDX_BACKFILL_COUNT", "800"))
    workers = int(env("TDX_BACKFILL_WORKERS", "8"))
    retries = int(env("TDX_RETRIES", "3"))
    retry_sleep = float(env("TDX_RETRY_SLEEP_SECONDS", "5"))

    symbols = load_symbols()
    if not symbols:
        raise RuntimeError("no symbols found in stock_info")

    print(
        f"tdx {mode} start={start} pages={pages} count={count} symbols={len(symbols)} workers={workers} retries={retries}",
        flush=True,
    )
    total_ok = 0
    pending = symbols
    failed: list[tuple[str, str]] = []

    for attempt in range(retries + 1):
        if attempt > 0:
            print(f"retry attempt {attempt}/{retries} symbols={len(pending)}", flush=True)
            time.sleep(retry_sleep)

        ok, failed = run_batch(pending, start, pages, count, workers)
        total_ok += ok
        pending = [symbol for symbol, _ in failed]
        if not pending:
            failed = []
            break

    if failed:
        print(f"tdx {mode} finished with failures: {len(failed)}", file=sys.stderr)
        return 1

    print(f"tdx {mode} finished ok={total_ok}", flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"tdx backfill failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
