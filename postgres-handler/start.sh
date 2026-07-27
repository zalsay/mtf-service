#!/usr/bin/env bash

set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${PROJECT_DIR}/bin"
BIN_PATH="${BIN_DIR}/postgres-handler"
PID_FILE="${PROJECT_DIR}/postgres-handler.pid"
LOG_FILE="${PROJECT_DIR}/postgres-handler.log"
ENV_FILE="${POSTGRES_HANDLER_ENV_FILE:-${PROJECT_DIR}/.env}"

cd "${PROJECT_DIR}"

load_env() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "Environment file not found: ${ENV_FILE}" >&2
    echo "Create it from .env.example and configure the PostgreSQL connection." >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
}

read_pid() {
  if [[ ! -f "${PID_FILE}" ]]; then
    return 1
  fi
  tr -d '[:space:]' < "${PID_FILE}"
}

stop_service() {
  local pid
  if ! pid="$(read_pid)"; then
    return 0
  fi

  if [[ ! "${pid}" =~ ^[0-9]+$ ]]; then
    echo "Removing invalid postgres-handler PID file: ${PID_FILE}"
    rm -f "${PID_FILE}"
    return 0
  fi

  if kill -0 "${pid}" 2>/dev/null; then
    echo "Stopping postgres-handler pid=${pid}"
    kill "${pid}" 2>/dev/null || true
    for _ in {1..30}; do
      if ! kill -0 "${pid}" 2>/dev/null; then
        break
      fi
      sleep 0.2
    done
    if kill -0 "${pid}" 2>/dev/null; then
      echo "Force stopping postgres-handler pid=${pid}"
      kill -9 "${pid}" 2>/dev/null || true
    fi
  fi
  rm -f "${PID_FILE}"
}

build_binary() {
  mkdir -p "${BIN_DIR}"
  local target_goos="${POSTGRES_HANDLER_GOOS:-$(go env GOOS)}"
  local target_goarch="${POSTGRES_HANDLER_GOARCH:-$(go env GOARCH)}"
  echo "Building postgres-handler (${target_goos}/${target_goarch})"
  CGO_ENABLED="${CGO_ENABLED:-0}" \
    GOOS="${target_goos}" \
    GOARCH="${target_goarch}" \
    GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
    go build -trimpath -ldflags="-s -w" -o "${BIN_PATH}" .
  chmod 755 "${BIN_PATH}"
}

start_service() {
  load_env
  stop_service
  build_binary
  mkdir -p "${PROJECT_DIR}"

  local port="${POSTGRES_HANDLER_PORT:-58004}"
  local health_url="${POSTGRES_HANDLER_HEALTHCHECK_URL:-http://127.0.0.1:${port}/health}"

  echo "Starting postgres-handler on :${port}"
  if command -v setsid >/dev/null 2>&1; then
    PORT="${port}" nohup setsid "${BIN_PATH}" >> "${LOG_FILE}" 2>&1 < /dev/null &
  else
    PORT="${port}" nohup "${BIN_PATH}" >> "${LOG_FILE}" 2>&1 < /dev/null &
  fi
  local pid=$!
  printf '%s\n' "${pid}" > "${PID_FILE}"

  if command -v curl >/dev/null 2>&1; then
    echo "Waiting for postgres-handler health check: ${health_url}"
    local healthy=0
    for _ in {1..60}; do
      if ! kill -0 "${pid}" 2>/dev/null; then
        break
      fi
      if curl --fail --silent "${health_url}" >/dev/null; then
        healthy=1
        break
      fi
      sleep 1
    done
    if [[ "${healthy}" -ne 1 ]]; then
      echo "postgres-handler failed health check; inspect ${LOG_FILE}" >&2
      tail -80 "${LOG_FILE}" >&2 || true
      kill "${pid}" 2>/dev/null || true
      rm -f "${PID_FILE}"
      exit 1
    fi
  fi

  echo "postgres-handler is healthy: ${health_url}"
  echo "pid=${pid}"
  echo "binary=${BIN_PATH}"
  echo "log=${LOG_FILE}"
}

status_service() {
  local pid
  if ! pid="$(read_pid)"; then
    echo "postgres-handler is stopped"
    return 1
  fi
  if kill -0 "${pid}" 2>/dev/null; then
    echo "postgres-handler is running: pid=${pid}"
    return 0
  fi
  echo "postgres-handler is not running; removing stale PID file"
  rm -f "${PID_FILE}"
  return 1
}

show_logs() {
  if [[ ! -f "${LOG_FILE}" ]]; then
    echo "No postgres-handler log yet: ${LOG_FILE}"
    return 0
  fi
  tail -n 80 -f "${LOG_FILE}"
}

show_help() {
  cat <<'EOF'
Usage: ./start.sh [start|stop|restart|status|logs|build]

  start    Build and start the service, stopping the previous PID first.
  stop     Stop the service recorded in postgres-handler.pid.
  restart  Rebuild and restart the service.
  status   Show the process status.
  logs     Follow the last 80 lines of postgres-handler.log.
  build    Build bin/postgres-handler without starting it.

Environment:
  POSTGRES_HANDLER_PORT       Local HTTP port, default 58004.
  POSTGRES_HANDLER_ENV_FILE   Environment file, default ./.env.
EOF
}

case "${1:-start}" in
  start|restart)
    start_service
    ;;
  stop)
    stop_service
    ;;
  status)
    status_service
    ;;
  logs)
    show_logs
    ;;
  build)
    build_binary
    ;;
  help|-h|--help)
    show_help
    ;;
  *)
    echo "Unknown command: $1" >&2
    show_help >&2
    exit 2
    ;;
esac
