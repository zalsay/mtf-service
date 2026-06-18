#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-daily}"
ROOT="/app"
CONFIG_FILE="${ROOT}/config.toml"

configure_fetcher() {
  python3 - "$CONFIG_FILE" <<'PY'
import os
import re
import sys

path = sys.argv[1]
content = open(path, "r", encoding="utf-8").read()

replacements = {
    "url": os.environ.get("CLICKHOUSE_URL", "http://a-stock-clickhouse:8123"),
    "database": os.environ.get("CLICKHOUSE_DATABASE", "stock_db"),
    "username": os.environ.get("CLICKHOUSE_USER", "stock_user"),
    "password": os.environ.get("CLICKHOUSE_PASSWORD", "stock_pass"),
}

in_clickhouse = False
lines = []
for line in content.splitlines():
    stripped = line.strip()
    if stripped.startswith("[") and stripped.endswith("]"):
        in_clickhouse = stripped == "[clickhouse]"
    if in_clickhouse:
        for key, value in replacements.items():
            if re.match(rf"^{key}\s*=", stripped):
                line = re.sub(r'=\s*".*"', f'= "{value}"', line)
                break
    lines.append(line)

open(path, "w", encoding="utf-8").write("\n".join(lines) + "\n")
PY
}

wait_clickhouse() {
  local deadline=$((SECONDS + ${CLICKHOUSE_WAIT_SECONDS:-120}))
  local endpoint="${CLICKHOUSE_URL}?user=${CLICKHOUSE_USER}&password=${CLICKHOUSE_PASSWORD}&database=${CLICKHOUSE_DATABASE}"
  until curl -fsS "$endpoint" --data-binary "SELECT 1" >/dev/null; do
    if (( SECONDS >= deadline )); then
      echo "ClickHouse is not ready: ${CLICKHOUSE_URL}" >&2
      return 1
    fi
    sleep 2
  done
}

normalize_date() {
  local raw="$1"
  echo "${raw//-/}"
}

next_date() {
  date -u -d "$1 +1 day" +%Y%m%d
}

sync_postgres_symbols() {
  python3 /app/scripts/sync_postgres_symbols_to_clickhouse.py
}

reset_stock_universe() {
  local endpoint="${CLICKHOUSE_URL}?user=${CLICKHOUSE_USER}&password=${CLICKHOUSE_PASSWORD}&database=${CLICKHOUSE_DATABASE}"
  curl -fsS "$endpoint" --data-binary "TRUNCATE TABLE IF EXISTS stock_info" >/dev/null || true
}

prepare_stock_universe() {
  case "${STOCK_UNIVERSE:-all}" in
    all)
      reset_stock_universe
      ;;
    postgres)
      sync_postgres_symbols
      ;;
    keep|existing)
      ;;
    *)
      echo "invalid STOCK_UNIVERSE=${STOCK_UNIVERSE}; expected all|postgres|keep" >&2
      return 2
      ;;
  esac
}

download_and_export() {
  local ymd
  ymd="$(normalize_date "$1")"
  local concurrent="${2:-${DOWNLOAD_CONCURRENT:-50}}"
  local prepare_universe="${3:-true}"
  local force_arg=()
  if [[ "${FORCE_DOWNLOAD:-false}" == "true" ]]; then
    force_arg=(--force)
  fi

  if [[ "$prepare_universe" == "true" ]]; then
    prepare_stock_universe
  fi

  echo "download level1 date=${ymd} concurrent=${concurrent}"
  set +e
  bulk_download "$ymd" "$concurrent" "${force_arg[@]}"
  local status=$?
  set -e

  if [[ "$status" -ne 0 ]]; then
    echo "bulk_download exited with ${status}; skip postgres export for ${ymd}" >&2
    return "$status"
  fi

  if [[ "${EXPORT_TO_POSTGRES:-true}" == "true" ]]; then
    CONFIG_FILE="$CONFIG_FILE" export_daily_to_postgres "$ymd"
  else
    echo "skip postgres export for ${ymd} because EXPORT_TO_POSTGRES=false"
  fi
}

configure_fetcher
wait_clickhouse

case "$MODE" in
  daily)
    target="${2:-$(TZ="${TZ:-Asia/Shanghai}" date +%Y%m%d)}"
    download_and_export "$target" "${3:-${DOWNLOAD_CONCURRENT:-50}}"
    ;;
  range)
    start="$(normalize_date "${2:?start date is required}")"
    end="$(normalize_date "${3:?end date is required}")"
    concurrent="${4:-${DOWNLOAD_CONCURRENT:-50}}"
    current="$start"
    prepare_stock_universe
    while [[ "$current" -le "$end" ]]; do
      echo "range item date=${current}"
      download_and_export "$current" "$concurrent" false || true
      current="$(next_date "$current")"
    done
    ;;
  export)
    target="${2:?date is required}"
    CONFIG_FILE="$CONFIG_FILE" export_daily_to_postgres "$(normalize_date "$target")"
    ;;
  tdx-bars)
    symbol="${2:?symbol is required}"
    start="${3:-0}"
    pages="${4:-1}"
    count="${5:-800}"
    format="${6:-jsonl}"
    tdx_daily_bars "$symbol" "$start" "$pages" "$count" "$format"
    ;;
  tdx-backfill-symbol)
    symbol="${2:?symbol is required}"
    start="${3:-0}"
    pages="${4:-8}"
    count="${5:-800}"
    TDX_INSERT_CLICKHOUSE=true tdx_daily_bars "$symbol" "$start" "$pages" "$count" none
    ;;
  tdx-history-postgres|tdx-history-watchlist)
    sync_postgres_symbols
    python3 /app/scripts/tdx_backfill_symbols.py history "${2:-0}" "${3:-${TDX_BACKFILL_PAGES:-8}}" "${4:-${TDX_BACKFILL_COUNT:-800}}"
    eastmoney_adjusted_bars history "${TDX_START_DATE:-2010-01-01}" "${TDX_END_DATE:-20500101}"
    ;;
  tdx-daily-postgres|tdx-daily-watchlist)
    sync_postgres_symbols
    python3 /app/scripts/tdx_backfill_symbols.py daily 0 1 "${2:-${TDX_DAILY_COUNT:-10}}"
    eastmoney_adjusted_bars daily "$(TZ="${TZ:-Asia/Shanghai}" date +%Y%m%d)" "$(TZ="${TZ:-Asia/Shanghai}" date +%Y%m%d)"
    ;;
  *)
    echo "usage: run_once.sh daily [YYYYMMDD] [concurrent] | range START END [concurrent] | export YYYYMMDD | tdx-bars SYMBOL [start] [pages] [count] [none|jsonl|csv] | tdx-backfill-symbol SYMBOL [start] [pages] [count] | tdx-history-watchlist [start] [pages] [count] | tdx-daily-watchlist [count]" >&2
    exit 2
    ;;
esac
