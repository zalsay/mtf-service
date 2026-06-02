#!/usr/bin/env bash
set -euo pipefail

API_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="${PID_FILE:-$API_DIR/.fintrack-api.pid}"
LOG_DIR="${LOG_DIR:-$API_DIR/logs}"
LOG_FILE="${LOG_FILE:-$LOG_DIR/fintrack-api.log}"
STOP_TIMEOUT="${STOP_TIMEOUT:-10}"

read_configured_port() {
    local port=""
    if [[ -f "$API_DIR/.env" ]]; then
        port="$(awk -F= '
            /^[[:space:]]*(SERVER_PORT|PORT)[[:space:]]*=/ {
                value=$2
                gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
                gsub(/^"|"$/, "", value)
                gsub(/^'\''|'\''$/, "", value)
                print value
            }
        ' "$API_DIR/.env" | tail -n 1)"
    fi
    echo "${port:-59000}"
}

is_running() {
    local pid="$1"
    [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

stop_process() {
    local pid="$1"
    if ! is_running "$pid"; then
        return 0
    fi

    echo "Stopping fintrack-api pid=$pid"
    kill "$pid" 2>/dev/null || true

    local waited=0
    while is_running "$pid" && [[ "$waited" -lt "$STOP_TIMEOUT" ]]; do
        sleep 1
        waited=$((waited + 1))
    done

    if is_running "$pid"; then
        echo "Force killing fintrack-api pid=$pid"
        kill -9 "$pid" 2>/dev/null || true
    fi
}

stop_port_processes() {
    local port="$1"
    local pids=""

    if command -v lsof >/dev/null 2>&1; then
        pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
    elif command -v fuser >/dev/null 2>&1; then
        pids="$(fuser -n tcp "$port" 2>/dev/null || true)"
    fi

    for pid in $pids; do
        if [[ "$pid" =~ ^[0-9]+$ ]] && [[ "$pid" != "$$" ]]; then
            stop_process "$pid"
        fi
    done
}

cd "$API_DIR"
mkdir -p "$LOG_DIR"

if [[ -f "$PID_FILE" ]]; then
    old_pid="$(tr -d '[:space:]' < "$PID_FILE")"
    if [[ "$old_pid" =~ ^[0-9]+$ ]]; then
        stop_process "$old_pid"
    fi
    rm -f "$PID_FILE"
fi

server_port="$(read_configured_port)"
stop_port_processes "$server_port"

echo "Starting fintrack-api with go run ."
nohup go run . >> "$LOG_FILE" 2>&1 &
new_pid="$!"
echo "$new_pid" > "$PID_FILE"

sleep 1
if ! is_running "$new_pid"; then
    echo "fintrack-api failed to start. Last logs:"
    tail -n 80 "$LOG_FILE" || true
    rm -f "$PID_FILE"
    exit 1
fi

echo "fintrack-api started pid=$new_pid"
echo "pid file: $PID_FILE"
echo "log file: $LOG_FILE"
