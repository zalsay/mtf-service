#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

START_DATE="${START_DATE:-20100101}"
END_DATE="${END_DATE:-$(TZ="${TZ:-Asia/Shanghai}" date +%Y%m%d)}"
MIN_SUPPORTED_DATE="${MIN_SUPPORTED_DATE:-20150105}"
CONCURRENT="${CONCURRENT:-${DOWNLOAD_CONCURRENT:-50}}"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-a-stock-level1-daily}"
SERVICE_NAME="${SERVICE_NAME:-a-stock-daily}"
CONTAINER_NAME="${CONTAINER_NAME:-a-stock-daily-history-backfill}"
CLICKHOUSE_DATABASE="${CLICKHOUSE_DATABASE:-stock_db}"
EXPORT_TO_POSTGRES="${EXPORT_TO_POSTGRES:-false}"
STOCK_UNIVERSE="${STOCK_UNIVERSE:-all}"
FORCE_DOWNLOAD="${FORCE_DOWNLOAD:-false}"
USE_HOST_PROXY="${USE_HOST_PROXY:-auto}"
HOST_PROXY_URL="${HOST_PROXY_URL:-http://host.docker.internal:7890}"
NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,a-stock-clickhouse,host.docker.internal}"

normalize_date() {
  local raw="$1"
  raw="${raw//-/}"
  if [[ ! "$raw" =~ ^[0-9]{8}$ ]]; then
    echo "invalid date: $1, expected YYYYMMDD or YYYY-MM-DD" >&2
    return 2
  fi
  echo "$raw"
}

container_exists() {
  docker ps -a --format '{{.Names}}' | grep -Fxq "$1"
}

host_proxy_available() {
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 7890 >/dev/null 2>&1
    return $?
  fi

  timeout 1 bash -c '</dev/tcp/127.0.0.1/7890' >/dev/null 2>&1
}

START_DATE="$(normalize_date "$START_DATE")"
END_DATE="$(normalize_date "$END_DATE")"
MIN_SUPPORTED_DATE="$(normalize_date "$MIN_SUPPORTED_DATE")"
RUN_START_DATE="$START_DATE"

if [[ "$RUN_START_DATE" < "$MIN_SUPPORTED_DATE" ]]; then
  RUN_START_DATE="$MIN_SUPPORTED_DATE"
fi

cd "$PROJECT_DIR"

echo "backfill all A stocks requested from ${START_DATE} to ${END_DATE}"
echo "effective_start_date=${RUN_START_DATE} min_supported_date=${MIN_SUPPORTED_DATE}"
echo "compose_project=${COMPOSE_PROJECT} service=${SERVICE_NAME} container=${CONTAINER_NAME}"
echo "clickhouse_database=${CLICKHOUSE_DATABASE} concurrent=${CONCURRENT} export_to_postgres=${EXPORT_TO_POSTGRES}"

docker compose -p "$COMPOSE_PROJECT" up -d a-stock-clickhouse
docker compose -p "$COMPOSE_PROJECT" build "$SERVICE_NAME"

if container_exists "$CONTAINER_NAME"; then
  echo "replace existing backfill container: ${CONTAINER_NAME}"
  docker stop "$CONTAINER_NAME" >/dev/null || true
  docker rm "$CONTAINER_NAME" >/dev/null
fi

env_args=(
  -e "CLICKHOUSE_DATABASE=${CLICKHOUSE_DATABASE}"
  -e "EXPORT_TO_POSTGRES=${EXPORT_TO_POSTGRES}"
  -e "STOCK_UNIVERSE=${STOCK_UNIVERSE}"
  -e "FORCE_DOWNLOAD=${FORCE_DOWNLOAD}"
  -e "DOWNLOAD_CONCURRENT=${CONCURRENT}"
)

if [[ "$USE_HOST_PROXY" == "true" ]] || { [[ "$USE_HOST_PROXY" == "auto" ]] && host_proxy_available; }; then
  echo "use host proxy: ${HOST_PROXY_URL}"
  env_args+=(
    -e "HTTP_PROXY=${HOST_PROXY_URL}"
    -e "HTTPS_PROXY=${HOST_PROXY_URL}"
    -e "http_proxy=${HOST_PROXY_URL}"
    -e "https_proxy=${HOST_PROXY_URL}"
    -e "NO_PROXY=${NO_PROXY}"
    -e "no_proxy=${NO_PROXY}"
  )
fi

docker compose -p "$COMPOSE_PROJECT" run -d \
  --name "$CONTAINER_NAME" \
  "${env_args[@]}" \
  "$SERVICE_NAME" range "$RUN_START_DATE" "$END_DATE" "$CONCURRENT"

echo "started ${CONTAINER_NAME}"
echo "logs: docker logs -f ${CONTAINER_NAME}"
