#!/bin/bash
# ─────────────────────────────────────────────────────────────────────────────
# SSH VPN Tunnel Server — Gateway Reverse Tunnel
#
# Creates a persistent reverse SSH port-forward from a public gateway server
# back to this container's SSH port.
#
# Flow:
#   VPN Client → gateway-public-ip:GW_EXPOSED_PORT
#                   └─(reverse tunnel)─► this container:SSH_PORT
#
# Requirements on the public gateway's sshd_config:
#   GatewayPorts yes          ← allows binding 0.0.0.0 for external access
#   AllowTcpForwarding yes    ← allows port forwarding
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Resolve env vars ──────────────────────────────────────────────────────────
GW_ENABLED="${GW_ENABLED:-no}"
GW_HOST="${GW_HOST:-}"
GW_PORT="${GW_PORT:-22}"
GW_USER="${GW_USER:-root}"
GW_SSH_KEY_FILE="${GW_SSH_KEY_FILE:-}"
GW_SSH_KEY="${GW_SSH_KEY:-}"
GW_SSH_KEY_B64="${GW_SSH_KEY_B64:-no}"
GW_PASSWORD="${GW_PASSWORD:-}"
GW_EXPOSED_PORT="${GW_EXPOSED_PORT:-2255}"
GW_BIND_ADDR="${GW_BIND_ADDR:-0.0.0.0}"
GW_RECONNECT_INTERVAL="${GW_RECONNECT_INTERVAL:-30}"
SSH_PORT="${SSH_PORT:-2222}"

log()     { echo "[gateway-tunnel] $(date '+%Y-%m-%d %H:%M:%S') $*"; }
err()     { echo "[gateway-tunnel] $(date '+%Y-%m-%d %H:%M:%S') ERROR: $*" >&2; }

# ── Skip if not enabled ───────────────────────────────────────────────────────
if [[ "${GW_ENABLED}" != "yes" ]]; then
    log "GW_ENABLED=no — gateway reverse tunnel disabled"
    exit 0
fi

# ── Validate required vars ────────────────────────────────────────────────────
if [[ -z "${GW_HOST}" ]]; then
    err "GW_HOST is not set. Gateway tunnel cannot start."
    exit 1
fi

# ── Resolve SSH key ───────────────────────────────────────────────────────────
KEY_FILE=""
KEY_TEMP=""

cleanup() {
    [[ -n "${KEY_TEMP}" ]] && rm -f "${KEY_TEMP}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

if [[ -n "${GW_SSH_KEY_FILE}" ]]; then
    # Mounted key file (preferred — e.g. docker secret or bind mount)
    if [[ ! -f "${GW_SSH_KEY_FILE}" ]]; then
        err "GW_SSH_KEY_FILE='${GW_SSH_KEY_FILE}' not found. Mount the key file into the container."
        exit 1
    fi
    # Docker on Windows mounts files with 0555 permissions — SSH rejects those.
    # Copy to a private temp file so we can chmod 600 regardless of mount perms.
    KEY_TEMP=$(mktemp /tmp/.gw_key.XXXXXX)
    cp "${GW_SSH_KEY_FILE}" "${KEY_TEMP}"
    chmod 600 "${KEY_TEMP}"
    KEY_FILE="${KEY_TEMP}"
    log "Using key file: ${GW_SSH_KEY_FILE} (copied to temp for permissions fix)"

elif [[ -n "${GW_SSH_KEY}" ]]; then
    # Key content from env var — write to a secure temp file
    KEY_TEMP=$(mktemp /tmp/.gw_key.XXXXXX)
    chmod 600 "${KEY_TEMP}"

    if [[ "${GW_SSH_KEY_B64}" == "yes" ]]; then
        # Base64-encoded key (safe for env vars — no newline issues)
        echo "${GW_SSH_KEY}" | base64 -d > "${KEY_TEMP}"
        log "Decoded base64 key from GW_SSH_KEY"
    else
        # Raw PEM — replace literal \n sequences with real newlines
        printf '%b' "${GW_SSH_KEY}" > "${KEY_TEMP}"
        log "Loaded key from GW_SSH_KEY"
    fi
    KEY_FILE="${KEY_TEMP}"

elif [[ -z "${GW_PASSWORD}" ]]; then
    err "No authentication method set. Provide one of:"
    err "  GW_SSH_KEY_FILE  — path to a mounted private key file"
    err "  GW_SSH_KEY       — raw PEM key content (use GW_SSH_KEY_B64=yes for base64)"
    err "  GW_PASSWORD      — password (testing only, requires sshpass)"
    exit 1
fi

# ── Common SSH options ────────────────────────────────────────────────────────
# -N        : no remote command (tunnel only)
# -T        : no pseudo-terminal
# ExitOnForwardFailure: fail fast if the remote port is already in use
SSH_BASE_OPTS=(
    -N
    -T
    -p "${GW_PORT}"
    -o StrictHostKeyChecking=accept-new
    -o ServerAliveInterval=30
    -o ServerAliveCountMax=3
    -o ExitOnForwardFailure=yes
    -o ConnectTimeout=30
)

# Reverse port-forward: gateway:GW_EXPOSED_PORT → localhost:SSH_PORT
REMOTE_FWD="${GW_BIND_ADDR}:${GW_EXPOSED_PORT}:localhost:${SSH_PORT}"

# ── Summary ───────────────────────────────────────────────────────────────────
log "──────────────────────────────────────────────────────"
log "Gateway reverse tunnel starting"
log "  Gateway:   ${GW_USER}@${GW_HOST}:${GW_PORT}"
log "  Exposing:  ${GW_HOST}:${GW_EXPOSED_PORT} → this container:${SSH_PORT}"
log "  Auth:      $(if [[ -n "${KEY_FILE}" ]]; then echo "SSH key (${KEY_FILE})"; else echo "Password (sshpass)"; fi)"
log "  Reconnect: every ${GW_RECONNECT_INTERVAL}s on failure"
log ""
log "  VPN clients connect to:"
log "    ssh -w 0:0 -o Tunnel=point-to-point root@${GW_HOST} -p ${GW_EXPOSED_PORT}"
log "──────────────────────────────────────────────────────"

# ── Kill any stale process holding GW_EXPOSED_PORT on the gateway ─────────────
# When this container restarts the old reverse-tunnel sshd child on the gateway
# keeps listening on GW_EXPOSED_PORT until its parent session times out.
# We loop-kill until the port is free so autossh can bind immediately.
kill_stale_port() {
    [[ -n "${KEY_FILE}" ]] || return 0
    local ssh_opts=(-o StrictHostKeyChecking=accept-new -o BatchMode=yes
                    -o ConnectTimeout=15 -p "${GW_PORT}")
    local max_attempts=8 attempt=0

    while (( attempt < max_attempts )); do
        (( attempt++ )) || true
        local pids
        pids=$(ssh "${ssh_opts[@]}" -i "${KEY_FILE}" "${GW_USER}@${GW_HOST}" \
            "sudo ss -Htnlp 'sport = :${GW_EXPOSED_PORT}' 2>/dev/null \
             | sed -n 's/.*pid=\([0-9]*\).*/\1/p'" 2>/dev/null || true)
        if [[ -z "${pids}" ]]; then
            log "Port ${GW_EXPOSED_PORT} on gateway is free"
            return 0
        fi
        log "Stale process(es) holding port ${GW_EXPOSED_PORT}: ${pids} — killing (attempt ${attempt}/${max_attempts})"
        # Try both with and without sudo (process may be owned by ansible or root)
        ssh "${ssh_opts[@]}" -i "${KEY_FILE}" "${GW_USER}@${GW_HOST}" \
            "kill ${pids} 2>/dev/null; sudo kill ${pids} 2>/dev/null; true" 2>/dev/null || true
        sleep 3
    done
    log "WARNING: Could not free port ${GW_EXPOSED_PORT} after ${max_attempts} attempts — continuing anyway"
}
kill_stale_port

# ── Connect loop ──────────────────────────────────────────────────────────────
attempt=0
while true; do
    (( attempt++ )) || true
    log "Attempt #${attempt}: connecting to ${GW_USER}@${GW_HOST}:${GW_PORT} ..."

    exit_code=0
    if [[ -n "${KEY_FILE}" ]]; then
        # Key-based auth — use autossh for automatic reconnection
        # -M 0: disable autossh monitoring port (rely on ServerAlive instead)
        # AUTOSSH_LOGFILE=/dev/stderr: autossh logs to stderr (captured in our log file)
        AUTOSSH_GATETIME=0 AUTOSSH_POLL=30 AUTOSSH_LOGLEVEL=7 AUTOSSH_LOGFILE=/dev/stderr \
        autossh -M 0 \
            "${SSH_BASE_OPTS[@]}" \
            -i "${KEY_FILE}" \
            -R "${REMOTE_FWD}" \
            "${GW_USER}@${GW_HOST}" || exit_code=$?
    else
        # Password auth — sshpass + plain ssh in a retry loop
        # Note: autossh cannot be used with sshpass (no tty for password)
        sshpass -p "${GW_PASSWORD}" \
        ssh "${SSH_BASE_OPTS[@]}" \
            -o BatchMode=no \
            -R "${REMOTE_FWD}" \
            "${GW_USER}@${GW_HOST}" || exit_code=$?
    fi

    log "Tunnel exited with code ${exit_code}. Retrying in ${GW_RECONNECT_INTERVAL}s ..."
    sleep "${GW_RECONNECT_INTERVAL}"
done
