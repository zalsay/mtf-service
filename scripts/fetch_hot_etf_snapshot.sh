#!/usr/bin/env bash
set -euo pipefail

# 用法:
#   HOT_ETF_SNAPSHOT_DIR=/var/www/fintrack/hot-etf ./scripts/fetch_hot_etf_snapshot.sh
#   HOT_ETF_SNAPSHOT_DIR=/var/www/fintrack/hot-etf ./scripts/fetch_hot_etf_snapshot.sh --now
#
# crontab 示例，每天凌晨 0 点抓取一次:
#   0 0 * * * HOT_ETF_SNAPSHOT_DIR=/var/www/fintrack/hot-etf /projects/ai-fin/fintrack/scripts/fetch_hot_etf_snapshot.sh >> /var/log/fintrack-hot-etf.log 2>&1
#
# 后端环境变量示例:
#   HOT_ETF_SNAPSHOT_URL=https://your-domain.example/hot-etf/latest.html
#   HOT_ETF_CACHE_DIR=/var/cache/fintrack/hot-etf

show_usage() {
  cat <<'USAGE'
Usage: fetch_hot_etf_snapshot.sh [--now]

Options:
  --now       Run one fetch immediately. This is explicit but equivalent to the default cron action.
  -h, --help  Show this help message.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --now)
      shift
      ;;
    -h|--help)
      show_usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      show_usage >&2
      exit 2
      ;;
  esac
done

SOURCE_URL="${HOT_ETF_SOURCE_URL:-https://etf.imlam.com/}"
SNAPSHOT_DIR="${HOT_ETF_SNAPSHOT_DIR:-/var/www/fintrack/hot-etf}"
USER_AGENT="${HOT_ETF_USER_AGENT:-Mozilla/5.0 (compatible; FinTrackHotETF/1.0)}"
TODAY="$(date +%F)"

mkdir -p "$SNAPSHOT_DIR"

tmp_file="$(mktemp "${SNAPSHOT_DIR}/hot-etf-${TODAY}.XXXXXX.tmp")"
dated_file="${SNAPSHOT_DIR}/hot-etf-${TODAY}.html"
latest_file="${SNAPSHOT_DIR}/latest.html"

cleanup() {
  rm -f "$tmp_file"
}
trap cleanup EXIT

curl \
  --fail \
  --silent \
  --show-error \
  --location \
  --retry 3 \
  --retry-delay 5 \
  --connect-timeout 20 \
  --max-time 120 \
  --user-agent "$USER_AGENT" \
  "$SOURCE_URL" \
  --output "$tmp_file"

if [ ! -s "$tmp_file" ]; then
  echo "hot ETF snapshot is empty: $SOURCE_URL" >&2
  exit 1
fi

cp "$tmp_file" "$dated_file"
cp "$tmp_file" "$latest_file"

echo "saved hot ETF snapshot: $latest_file"
