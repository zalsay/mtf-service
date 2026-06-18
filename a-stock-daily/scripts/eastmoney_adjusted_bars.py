#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sys
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed


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
    return [json.loads(line)["code"] for line in text.splitlines() if line.strip()]


def secid(symbol: str) -> str:
    market = "1" if symbol.startswith(("6", "5")) else "0"
    return f"{market}.{symbol}"


def fqt(adjust: str) -> str:
    if adjust == "qfq":
        return "1"
    if adjust == "hfq":
        return "2"
    return "0"


def fetch_symbol(symbol: str, adjust: str, start_date: str, end_date: str) -> list[dict[str, object]]:
    params = urllib.parse.urlencode(
        {
            "fields1": "f1,f2,f3,f4,f5,f6",
            "fields2": "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61",
            "klt": "101",
            "fqt": fqt(adjust),
            "beg": start_date,
            "end": end_date,
            "secid": secid(symbol),
        }
    )
    url = f"https://push2his.eastmoney.com/api/qt/stock/kline/get?{params}"
    request = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(request, timeout=int(env("EASTMONEY_TIMEOUT_SECONDS", "30"))) as response:
        payload = json.loads(response.read().decode("utf-8"))

    klines = ((payload.get("data") or {}).get("klines") or [])
    rows: list[dict[str, object]] = []
    for line in klines:
        parts = str(line).split(",")
        if len(parts) < 7:
            continue
        trade_date = parts[0]
        rows.append(
            {
                "code": symbol,
                "adjust": adjust,
                "trade_date": trade_date,
                "datetime": f"{trade_date} 15:00:00",
                "date_str": trade_date,
                "open": float(parts[1] or 0),
                "close": float(parts[2] or 0),
                "high": float(parts[3] or 0),
                "low": float(parts[4] or 0),
                "volume": int(float(parts[5] or 0)),
                "amount": float(parts[6] or 0),
                "source": "eastmoney",
            }
        )
    return rows


def ensure_table() -> None:
    clickhouse_query(
        """
CREATE TABLE IF NOT EXISTS tdx_daily_bars
(
    code String,
    adjust String DEFAULT 'none',
    trade_date Date,
    datetime String,
    date_str String,
    open Float64,
    close Float64,
    high Float64,
    low Float64,
    volume UInt64,
    amount Float64,
    source String DEFAULT 'tdx',
    updated_at DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (code, adjust, trade_date)
"""
    )


def insert_rows(rows: list[dict[str, object]]) -> None:
    if not rows:
        return
    payload = "\n".join(json.dumps(row, ensure_ascii=False, separators=(",", ":")) for row in rows)
    clickhouse_query(f"INSERT INTO tdx_daily_bars FORMAT JSONEachRow\n{payload}\n")


def run_symbol(symbol: str, adjusts: list[str], start_date: str, end_date: str) -> tuple[str, int, str]:
    try:
        total = 0
        for adjust in adjusts:
            rows = fetch_symbol(symbol, adjust, start_date, end_date)
            insert_rows(rows)
            total += len(rows)
        return symbol, 0, f"inserted {total}"
    except Exception as exc:
        return symbol, 1, str(exc)


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "history"
    start_date = (sys.argv[2] if len(sys.argv) > 2 else env("TDX_START_DATE", "2010-01-01")).replace("-", "")
    end_date = (sys.argv[3] if len(sys.argv) > 3 else env("TDX_END_DATE", "20500101")).replace("-", "")
    adjusts = [item.strip() for item in env("ADJUSTED_BARS_ADJUSTS", "qfq,hfq").split(",") if item.strip()]
    workers = int(env("ADJUSTED_BARS_WORKERS", env("TDX_BACKFILL_WORKERS", "8")))
    retries = int(env("TDX_RETRIES", "3"))

    ensure_table()
    symbols = load_symbols()
    if not symbols:
        raise RuntimeError("no symbols found in stock_info")

    print(f"eastmoney {mode} start={start_date} end={end_date} adjusts={','.join(adjusts)} symbols={len(symbols)} workers={workers}", flush=True)
    pending = symbols
    total_ok = 0
    failed: list[tuple[str, str]] = []
    for attempt in range(retries + 1):
        failed = []
        started = time.time()
        with ThreadPoolExecutor(max_workers=workers) as pool:
            futures = [pool.submit(run_symbol, symbol, adjusts, start_date, end_date) for symbol in pending]
            for index, future in enumerate(as_completed(futures), start=1):
                symbol, code, output = future.result()
                if code == 0:
                    total_ok += 1
                else:
                    failed.append((symbol, output))
                    print(f"failed {symbol}: {output}", file=sys.stderr, flush=True)
                if index % 50 == 0 or index == len(pending):
                    elapsed = max(0.1, time.time() - started)
                    print(f"progress {index}/{len(pending)} ok={index-len(failed)} failed={len(failed)} speed={index/elapsed:.2f}/s", flush=True)
        if not failed:
            print(f"eastmoney {mode} finished ok={total_ok}", flush=True)
            return 0
        pending = [symbol for symbol, _ in failed]
        print(f"retry attempt {attempt + 1}/{retries} symbols={len(pending)}", flush=True)
        time.sleep(float(env("TDX_RETRY_SLEEP_SECONDS", "5")))

    print(f"eastmoney {mode} finished with failures: {len(failed)}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"eastmoney adjusted bars failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
