#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any


def env(name: str, default: str) -> str:
    return os.environ.get(name, default).strip() or default


def postgres_request(path: str) -> dict[str, Any]:
    base = env("POSTGRES_HANDLER_URL", "http://pos-handler:8080").rstrip("/")
    request = urllib.request.Request(f"{base}{path}", method="GET")
    token = os.environ.get("POSTGRES_HANDLER_TOKEN", "").strip()
    if token:
        request.add_header("X-Token", token)

    try:
        with urllib.request.urlopen(request, timeout=int(env("POSTGRES_TIMEOUT", "60"))) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"postgres-handler returned HTTP {exc.code}: {detail}") from exc


def clickhouse_endpoint() -> str:
    base = env("CLICKHOUSE_URL", "http://a-stock-clickhouse:8123")
    params = urllib.parse.urlencode(
        {
            "user": env("CLICKHOUSE_USER", "stock_user"),
            "password": env("CLICKHOUSE_PASSWORD", "stock_pass"),
            "database": env("CLICKHOUSE_DATABASE", "stock_db"),
        }
    )
    return f"{base}/?{params}"


def clickhouse_exec(sql: str) -> str:
    request = urllib.request.Request(
        clickhouse_endpoint(),
        data=sql.encode("utf-8"),
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=int(env("CLICKHOUSE_TIMEOUT", "60"))) as response:
        return response.read().decode("utf-8")


def normalize_symbol(value: Any) -> str:
    raw = str(value or "").strip()
    lower = raw.lower()
    for prefix in ("sh", "sz", "bj"):
        if lower.startswith(prefix) and len(raw) > len(prefix):
            raw = raw[len(prefix) :]
            break
    return raw


def is_supported_market_code(code: str) -> bool:
    return len(code) == 6 and code.isdigit() and code[0] in {"6", "5", "0", "1", "3"}


def is_supported_stock_type(stock_type: Any) -> bool:
    try:
        return int(stock_type or 0) in {1, 2}
    except (TypeError, ValueError):
        return False


def infer_stock_type(code: str) -> int:
    if code.startswith(("5", "15", "16", "18")):
        return 2
    return 1


def stock_rows(items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    rows: dict[str, dict[str, Any]] = {}
    for item in items:
        if not is_supported_stock_type(item.get("stock_type")):
            continue
        code = normalize_symbol(item.get("symbol"))
        if not is_supported_market_code(code):
            continue
        stock_type = int(item.get("stock_type") or infer_stock_type(code))
        rows[code] = {
            "code": code,
            "stock_type": stock_type,
            "name": str(item.get("name") or "").strip(),
            "industry_l1_code": "",
            "industry_l1_name": "",
            "industry_l2_code": "",
            "industry_l2_name": "",
            "industry_l3_code": "",
            "industry_l3_name": "",
            "industry_l4_code": "",
            "industry_l4_name": "",
        }
    return [rows[code] for code in sorted(rows)]


def stock_rows_from_symbols(items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    rows: dict[str, dict[str, Any]] = {}
    for item in items:
        code = normalize_symbol(item.get("symbol"))
        if not is_supported_market_code(code):
            continue
        stock_type = int(item.get("stock_type") or infer_stock_type(code))
        if stock_type not in {1, 2}:
            continue
        rows[code] = {
            "code": code,
            "stock_type": stock_type,
            "name": "",
            "industry_l1_code": "",
            "industry_l1_name": "",
            "industry_l2_code": "",
            "industry_l2_name": "",
            "industry_l3_code": "",
            "industry_l3_name": "",
            "industry_l4_code": "",
            "industry_l4_name": "",
        }
    return [rows[code] for code in sorted(rows)]


def load_watchlist_symbols() -> list[dict[str, Any]]:
    dsn = env("FINTRACK_DATABASE_URL", "")
    if not dsn:
        host = env("FINTRACK_DB_HOST", "host.docker.internal")
        port = env("FINTRACK_DB_PORT", "5435")
        user = env("FINTRACK_DB_USER", "cc_user")
        password = env("FINTRACK_DB_PASSWORD", "cc_pass_2026")
        name = env("FINTRACK_DB_NAME", "cc_service")
        sslmode = env("FINTRACK_DB_SSLMODE", "disable")
        dsn = f"postgresql://{urllib.parse.quote(user)}:{urllib.parse.quote(password)}@{host}:{port}/{name}?sslmode={sslmode}"

    sql = """
SELECT regexp_replace(lower(symbol), '^(sh|sz|bj)', '') AS code,
       max(COALESCE(stock_type, 1)) AS stock_type
FROM user_watchlist
WHERE COALESCE(stock_type, 1) IN (1, 2)
  AND symbol IS NOT NULL
  AND regexp_replace(lower(symbol), '^(sh|sz|bj)', '') ~ '^[01356][0-9]{5}$'
GROUP BY code
ORDER BY code
"""
    completed = subprocess.run(
        ["psql", dsn, "-At", "-c", sql],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=int(env("FINTRACK_DB_TIMEOUT", "60")),
        check=False,
    )
    if completed.returncode != 0:
        raise RuntimeError(f"failed to query watchlist symbols: {completed.stderr.strip()}")
    items: list[dict[str, Any]] = []
    for line in completed.stdout.splitlines():
        if not line.strip():
            continue
        code, _, stock_type = line.partition("|")
        items.append({"symbol": code.strip(), "stock_type": int(stock_type or 1)})
    return items


def create_stock_info_table() -> None:
    clickhouse_exec(
        f"CREATE DATABASE IF NOT EXISTS {env('CLICKHOUSE_DATABASE', 'stock_db')}"
    )
    clickhouse_exec(
        """
CREATE TABLE IF NOT EXISTS stock_info (
    code String,
    name String,
    industry_l1_code String,
    industry_l1_name String,
    industry_l2_code String,
    industry_l2_name String,
    industry_l3_code String,
    industry_l3_name String,
    industry_l4_code String,
    industry_l4_name String
) ENGINE = ReplacingMergeTree()
ORDER BY code
"""
    )
    clickhouse_exec(
        """
CREATE TABLE IF NOT EXISTS daily_symbol_info (
    code String,
    stock_type UInt8,
    updated_at DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY code
"""
    )


def stock_info_payload_rows(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    stock_info_keys = {
        "code",
        "name",
        "industry_l1_code",
        "industry_l1_name",
        "industry_l2_code",
        "industry_l2_name",
        "industry_l3_code",
        "industry_l3_name",
        "industry_l4_code",
        "industry_l4_name",
    }
    return [{key: row.get(key, "") for key in stock_info_keys} for row in rows]


def insert_stock_info(rows: list[dict[str, Any]]) -> None:
    payload_rows = stock_info_payload_rows(rows)
    payload = "\n".join(json.dumps(row, ensure_ascii=False, separators=(",", ":")) for row in payload_rows)
    clickhouse_exec("TRUNCATE TABLE stock_info")
    clickhouse_exec(f"INSERT INTO stock_info FORMAT JSONEachRow\n{payload}\n")
    symbol_payload = "\n".join(
        json.dumps({"code": row["code"], "stock_type": int(row["stock_type"])}, ensure_ascii=False, separators=(",", ":"))
        for row in rows
    )
    clickhouse_exec("TRUNCATE TABLE IF EXISTS daily_symbol_info")
    clickhouse_exec(f"INSERT INTO daily_symbol_info FORMAT JSONEachRow\n{symbol_payload}\n")


def main() -> int:
    source = env("SYMBOL_SOURCE", "watchlist")
    if source == "watchlist":
        rows = stock_rows_from_symbols(load_watchlist_symbols())
        source_label = "watchlist"
    elif source == "postgres":
        response = postgres_request("/api/v1/stock-data/symbols")
        data = response.get("data") or []
        if not isinstance(data, list):
            raise RuntimeError("postgres-handler symbols response data is not a list")
        rows = stock_rows(data)
        source_label = "postgres"
    else:
        raise RuntimeError(f"invalid SYMBOL_SOURCE={source}; expected watchlist|postgres")

    if not rows:
        raise RuntimeError(f"no supported stock/ETF symbols found in {source_label}")

    create_stock_info_table()
    insert_stock_info(rows)
    print(f"synced {len(rows)} {source_label} stock/ETF symbols into ClickHouse stock_info")
    return len(rows)

if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"sync postgres symbols failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
