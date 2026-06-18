#!/usr/bin/env bash
set -euo pipefail

mode="${1:-scheduler}"

if [[ "$mode" == "bash" || "$mode" == "sh" ]]; then
  exec "$@"
fi

case "$mode" in
  server|http|trigger)
    exec python3 /app/scripts/trigger_server.py
    ;;
  daily|range|export|tdx-bars|tdx-backfill-symbol|tdx-history-postgres|tdx-daily-postgres|tdx-history-watchlist|tdx-daily-watchlist)
    exec /app/scripts/run_once.sh "$@"
    ;;
  scheduler|cron)
    if [[ "${RUN_FULL_ON_START:-false}" == "true" ]]; then
      /app/scripts/run_once.sh range "${FULL_START_DATE:?FULL_START_DATE is required}" "${FULL_END_DATE:?FULL_END_DATE is required}" || true
    fi

    while true; do
      sleep_seconds="$(python3 /app/scripts/seconds_until.py "${DAILY_RUN_HOUR:-22}" "${DAILY_RUN_MINUTE:-0}")"
      echo "next daily run in ${sleep_seconds}s at ${DAILY_RUN_HOUR:-22}:${DAILY_RUN_MINUTE:-0} (${TZ:-Asia/Shanghai})"
      sleep "$sleep_seconds"
      if [[ "${DAILY_SYNC_MODE:-level1}" == "tdx" ]]; then
        /app/scripts/run_once.sh tdx-daily-watchlist || true
      else
        /app/scripts/run_once.sh daily || true
      fi
      sleep 60
    done
    ;;
  *)
    exec "$@"
    ;;
esac
