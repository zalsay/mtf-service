#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${PROJECT_DIR}/bin"
BIN_PATH="${BIN_DIR}/inference-gateway"
PID_FILE="${PROJECT_DIR}/gateway.pid"
LOG_FILE="${PROJECT_DIR}/gateway.log"
DEFAULT_ENV_FILE="${PROJECT_DIR}/.env"
EXAMPLE_ENV_FILE="${PROJECT_DIR}/.env.example"

cd "${PROJECT_DIR}"

load_env() {
  local env_file="${GATEWAY_ENV_FILE:-${DEFAULT_ENV_FILE}}"
  if [[ ! -f "${env_file}" ]]; then
    if [[ "${env_file}" == "${DEFAULT_ENV_FILE}" && -f "${EXAMPLE_ENV_FILE}" ]]; then
      cp "${EXAMPLE_ENV_FILE}" "${env_file}"
      echo "Created ${env_file} from ${EXAMPLE_ENV_FILE}; review it before production use."
    else
      echo "Environment file not found: ${env_file}" >&2
      exit 1
    fi
  fi

  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
}

read_pid() {
  if [[ ! -f "${PID_FILE}" ]]; then
    return 1
  fi
  tr -d '[:space:]' < "${PID_FILE}"
}

stop_previous() {
  local pid
  if ! pid="$(read_pid)"; then
    return
  fi

  if [[ ! "${pid}" =~ ^[0-9]+$ ]]; then
    echo "Removing invalid gateway PID file: ${PID_FILE}"
    rm -f "${PID_FILE}"
    return
  fi

  if ! kill -0 "${pid}" 2>/dev/null; then
    rm -f "${PID_FILE}"
    return
  fi

  echo "Stopping inference-gateway pid=${pid}"
  kill "${pid}" 2>/dev/null || true
  for _ in {1..30}; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      break
    fi
    sleep 0.2
  done
  if kill -0 "${pid}" 2>/dev/null; then
    echo "Force stopping inference-gateway pid=${pid}"
    kill -9 "${pid}" 2>/dev/null || true
  fi
  rm -f "${PID_FILE}"
}

build_binary() {
  mkdir -p "${BIN_DIR}"
  local target_goos="${GOOS:-$(go env GOOS)}"
  local target_goarch="${GOARCH:-$(go env GOARCH)}"
  echo "Building inference-gateway (${target_goos}/${target_goarch})"
  CGO_ENABLED="${CGO_ENABLED:-0}" \
    GOOS="${target_goos}" \
    GOARCH="${target_goarch}" \
    GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
    go build -trimpath -ldflags="-s -w" -o "${BIN_PATH}" ./cmd/inference-gateway
  chmod 755 "${BIN_PATH}"
}

start_service() {
  load_env
  stop_previous
  build_binary

  local port="${SERVICE_PORT:-9010}"
  local health_url="${GATEWAY_HEALTHCHECK_URL:-http://127.0.0.1:${port}/health}"
  mkdir -p "${PROJECT_DIR}/data"

  echo "Starting inference-gateway on :${port}"
  if command -v setsid >/dev/null 2>&1; then
    nohup setsid "${BIN_PATH}" >> "${LOG_FILE}" 2>&1 < /dev/null &
  else
    nohup "${BIN_PATH}" >> "${LOG_FILE}" 2>&1 < /dev/null &
  fi
  local pid=$!
  printf '%s\n' "${pid}" > "${PID_FILE}"

  if command -v curl >/dev/null 2>&1; then
    local healthy=0
    for _ in {1..60}; do
      if ! kill -0 "${pid}" 2>/dev/null; then
        break
      fi
      if curl --fail --silent --show-error "${health_url}" >/dev/null; then
        healthy=1
        break
      fi
      sleep 1
    done
    if [[ "${healthy}" -ne 1 ]]; then
      echo "inference-gateway failed health check; inspect ${LOG_FILE}" >&2
      tail -80 "${LOG_FILE}" >&2 || true
      kill "${pid}" 2>/dev/null || true
      rm -f "${PID_FILE}"
      exit 1
    fi
  else
    sleep 1
    if ! kill -0 "${pid}" 2>/dev/null; then
      echo "inference-gateway failed to start; inspect ${LOG_FILE}" >&2
      rm -f "${PID_FILE}"
      exit 1
    fi
  fi

  echo "inference-gateway is healthy: ${health_url}"
  echo "pid=${pid}"
  echo "binary=${BIN_PATH}"
  echo "log=${LOG_FILE}"
}

status_service() {
  local pid
  if ! pid="$(read_pid)"; then
    echo "inference-gateway is stopped"
    return 1
  fi
  if kill -0 "${pid}" 2>/dev/null; then
    echo "inference-gateway is running: pid=${pid}"
    return 0
  fi
  echo "inference-gateway is not running; removing stale PID file"
  rm -f "${PID_FILE}"
  return 1
}

show_logs() {
  if [[ ! -f "${LOG_FILE}" ]]; then
    echo "No gateway log yet: ${LOG_FILE}"
    return 0
  fi
  tail -n 80 -f "${LOG_FILE}"
}

show_help() {
  cat <<'EOF'
Usage: ./start.sh [start|stop|restart|status|logs|build]

  start    Build and start the gateway, stopping the previous PID first.
  stop     Stop the gateway recorded in gateway.pid.
  restart  Rebuild and restart the gateway.
  status   Show the process status.
  logs     Follow the last 80 lines of gateway.log.
  build    Build bin/inference-gateway without starting it.
EOF
}

case "${1:-start}" in
  start|restart)
    start_service
    ;;
  stop)
    stop_previous
    ;;
  status)
    status_service
    ;;
  logs)
    show_logs
    ;;
  build)
    load_env
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
