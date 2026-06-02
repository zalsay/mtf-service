#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

REMOTE_HOST="${REMOTE_HOST:-root@go-api.meetlife.com.cn}"
REMOTE_PORT="${REMOTE_PORT:-22}"
REMOTE_DIR="${REMOTE_DIR:-/root/workers/fintrack-api}"
REMOTE_CONTAINER="${REMOTE_CONTAINER:-fintrack-api}"
LOCAL_BINARY="${LOCAL_BINARY:-${SCRIPT_DIR}/run}"
REMOTE_TMP_NAME="${REMOTE_TMP_NAME:-run.codex.tmp}"
REMOTE_BINARY_NAME="${REMOTE_BINARY_NAME:-run}"
SSH_KNOWN_HOSTS_FILE="${SSH_KNOWN_HOSTS_FILE:-${HOME}/.ssh/known_hosts}"

log() {
    printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "missing required command: $1" >&2
        exit 1
    fi
}

require_cmd make
require_cmd scp
require_cmd ssh
require_cmd ssh-add

if [[ -z "${SSH_AUTH_SOCK:-}" ]]; then
    echo "SSH_AUTH_SOCK is not set; start ssh-agent and add a key first" >&2
    exit 1
fi

if ! ssh-add -l >/dev/null 2>&1; then
    echo "ssh-agent has no usable identities; run ssh-add first" >&2
    exit 1
fi

SSH_OPTS=(
    -o BatchMode=yes
    -o "IdentityAgent=${SSH_AUTH_SOCK}"
    -o "UserKnownHostsFile=${SSH_KNOWN_HOSTS_FILE}"
)
SCP_OPTS=(
    -o BatchMode=yes
    -o "IdentityAgent=${SSH_AUTH_SOCK}"
    -o "UserKnownHostsFile=${SSH_KNOWN_HOSTS_FILE}"
)

log "building local run binary"
make -C "${SCRIPT_DIR}" build

if [[ ! -f "${LOCAL_BINARY}" ]]; then
    echo "local binary not found: ${LOCAL_BINARY}" >&2
    exit 1
fi

log "uploading ${LOCAL_BINARY} to ${REMOTE_HOST}:${REMOTE_DIR}/${REMOTE_TMP_NAME}"
scp "${SCP_OPTS[@]}" -P "${REMOTE_PORT}" "${LOCAL_BINARY}" "${REMOTE_HOST}:${REMOTE_DIR}/${REMOTE_TMP_NAME}"

REMOTE_SCRIPT=$(cat <<EOF
set -euo pipefail
cd "${REMOTE_DIR}"
chmod 755 "${REMOTE_TMP_NAME}"
mv "${REMOTE_TMP_NAME}" "${REMOTE_BINARY_NAME}"
chmod 755 "${REMOTE_BINARY_NAME}"
docker restart "${REMOTE_CONTAINER}" >/dev/null
docker ps --filter "name=^/${REMOTE_CONTAINER}\$" --format "table {{.Names}}\t{{.Status}}"
EOF
)

log "replacing remote run binary and restarting ${REMOTE_CONTAINER}"
ssh "${SSH_OPTS[@]}" -p "${REMOTE_PORT}" "${REMOTE_HOST}" "${REMOTE_SCRIPT}"

log "fintrack-api deploy completed"
