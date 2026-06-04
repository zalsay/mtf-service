#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

REMOTE_HOST="${REMOTE_HOST:-root@go-api.meetlife.com.cn}"
REMOTE_PORT="${REMOTE_PORT:-22}"
REMOTE_BASE_DIR="${REMOTE_BASE_DIR:-/root/workers}"
SSH_KNOWN_HOSTS_FILE="${SSH_KNOWN_HOSTS_FILE:-${HOME}/.ssh/known_hosts}"
REMOTE_TMP_NAME="${REMOTE_TMP_NAME:-run.codex.tmp}"
REMOTE_BINARY_NAME="${REMOTE_BINARY_NAME:-run}"

log() {
    printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

usage() {
    cat <<'EOF'
Usage: ./deploy.sh [api|handler|all]

Targets:
  api      Build fintrack-api/run, upload to /root/workers/fintrack-api/run, restart fintrack-api
  handler  Build postgres-handler/run, upload to /root/workers/pg-handler/run, restart pos-handler
  all      Deploy api then handler

Environment overrides:
  REMOTE_HOST=root@go-api.meetlife.com.cn
  REMOTE_PORT=22
  REMOTE_BASE_DIR=/root/workers
  API_REMOTE_CONTAINER=fintrack-api
  HANDLER_REMOTE_CONTAINER=pos-handler
EOF
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing required command: $1" >&2
        exit 1
    fi
}

require_ssh_agent() {
    if [[ -z "${SSH_AUTH_SOCK:-}" ]]; then
        echo "SSH_AUTH_SOCK is not set; start ssh-agent and add a key first" >&2
        exit 1
    fi
    if ! ssh-add -l >/dev/null 2>&1; then
        echo "ssh-agent has no usable identities; run ssh-add first" >&2
        exit 1
    fi
}

deploy_service() {
    local name="$1"
    local local_dir="$2"
    local remote_dir="$3"
    local remote_container="$4"
    local local_binary="${local_dir}/run"

    log "building ${name}"
    make -C "${local_dir}" build

    if [[ ! -f "${local_binary}" ]]; then
        echo "local binary not found: ${local_binary}" >&2
        exit 1
    fi

    local ssh_opts=(
        -o BatchMode=yes
        -o "IdentityAgent=${SSH_AUTH_SOCK}"
        -o "UserKnownHostsFile=${SSH_KNOWN_HOSTS_FILE}"
    )
    local scp_opts=("${ssh_opts[@]}")

    log "ensuring remote directory ${REMOTE_HOST}:${remote_dir}"
    ssh "${ssh_opts[@]}" -p "${REMOTE_PORT}" "${REMOTE_HOST}" "mkdir -p '${remote_dir}'"

    log "uploading ${name} binary to ${REMOTE_HOST}:${remote_dir}/${REMOTE_TMP_NAME}"
    scp "${scp_opts[@]}" -P "${REMOTE_PORT}" "${local_binary}" "${REMOTE_HOST}:${remote_dir}/${REMOTE_TMP_NAME}"

    local remote_script
    remote_script=$(cat <<EOF
set -euo pipefail
cd "${remote_dir}"
chmod 755 "${REMOTE_TMP_NAME}"
mv "${REMOTE_TMP_NAME}" "${REMOTE_BINARY_NAME}"
chmod 755 "${REMOTE_BINARY_NAME}"
docker restart "${remote_container}" >/dev/null
docker ps --filter "name=^/${remote_container}\$" --format "table {{.Names}}\t{{.Status}}"
EOF
)

    log "replacing remote ${name} binary and restarting ${remote_container}"
    ssh "${ssh_opts[@]}" -p "${REMOTE_PORT}" "${REMOTE_HOST}" "${remote_script}"
    log "${name} deploy completed"
}

main() {
    local target="${1:-api}"
    if [[ "${target}" == "-h" || "${target}" == "--help" ]]; then
        usage
        exit 0
    fi

    require_cmd make
    require_cmd scp
    require_cmd ssh
    require_cmd ssh-add
    require_ssh_agent

    case "${target}" in
        api)
            deploy_service \
                "fintrack-api" \
                "${ROOT_DIR}/fintrack-api" \
                "${API_REMOTE_DIR:-${REMOTE_BASE_DIR}/fintrack-api}" \
                "${API_REMOTE_CONTAINER:-fintrack-api}"
            ;;
        handler)
            deploy_service \
                "postgres-handler" \
                "${ROOT_DIR}/postgres-handler" \
                "${HANDLER_REMOTE_DIR:-${REMOTE_BASE_DIR}/pg-handler}" \
                "${HANDLER_REMOTE_CONTAINER:-pos-handler}"
            ;;
        all)
            "$0" api
            "$0" handler
            ;;
        *)
            usage >&2
            exit 2
            ;;
    esac
}

main "$@"
