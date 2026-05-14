#!/bin/bash
# ─────────────────────────────────────────────────────────────────────────────
# VPN User Manager — add, remove, and list multi-user VPN accounts at runtime
#
# Usage (run inside the container via docker exec):
#   manage-users.sh add    <username> "<ssh-pubkey>"
#   manage-users.sh remove <username>
#   manage-users.sh list
#   manage-users.sh show   <username>
#
# Each user is assigned:
#   - A tun slot (1–253) → tun<N> on both client and server
#   - Server IP: 10.10.<N>.2  /  Client IP: 10.10.<N>.1
#   - An authorized_keys entry:  tunnel="<N>" <pubkey> vpn-user:<username>
#   - A config file: /etc/vpn/users/<username>.conf
#
# Client connect command is printed on add.
# The tun-monitor picks up the new config automatically (no restart needed).
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

USERS_DIR="/etc/vpn/users"
AUTH_KEYS="/root/.ssh/authorized_keys"
MAX_SLOT=253

log() { echo "[manage-users] $*"; }
err() { echo "[manage-users] ERROR: $*" >&2; exit 1; }

usage() {
    cat << 'EOF'
Usage:
  manage-users.sh add    <username> "<ssh-pubkey>"
  manage-users.sh remove <username>
  manage-users.sh list
  manage-users.sh show   <username>

IP scheme (per tun slot N):
  Server tun IP : 10.10.<N>.2
  Client tun IP : 10.10.<N>.1
  Tun device    : tun<N>  (on both client and server)

Client connect (after 'add'):
  sudo ./vpn-connect.sh --tun <N>            (split tunnel)
  sudo ./vpn-connect.sh --tun <N> --full-tunnel
EOF
    exit 1
}

mkdir -p "${USERS_DIR}"

# ── Find the next free tun slot (1–253) ──────────────────────────────────────
next_slot() {
    shopt -s nullglob
    for slot in $(seq 1 "${MAX_SLOT}"); do
        local used=0
        for conf in "${USERS_DIR}"/*.conf; do
            grep -q "^TUN_NUM=${slot}$" "$conf" && used=1 && break
        done
        [[ $used -eq 0 ]] && echo "$slot" && return
    done
    err "No free tun slots (max ${MAX_SLOT} users reached)"
}

cmd="${1:-}"
[[ -z "$cmd" ]] && usage

case "$cmd" in

    # ── add ──────────────────────────────────────────────────────────────────
    add)
        [[ $# -lt 3 ]] && usage
        username="$2"
        pubkey="$3"

        [[ "$username" =~ ^[a-zA-Z0-9_-]+$ ]] || \
            err "Invalid username '${username}' — only alphanumeric, - and _ are allowed"

        conf="${USERS_DIR}/${username}.conf"
        [[ -f "$conf" ]] && err "User '${username}' already exists. Run 'remove' first."

        echo "$pubkey" | grep -qE '^(ssh-|ecdsa-|sk-)' || \
            err "Invalid SSH public key (must start with ssh-rsa, ssh-ed25519, ecdsa-…)"

        slot=$(next_slot)
        server_ip="10.10.${slot}.2"
        client_ip="10.10.${slot}.1"

        # Write user config (read by tun-monitor and manage-users.sh)
        cat > "$conf" << EOF
# VPN user config — managed by manage-users.sh
# Created: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
USERNAME=${username}
TUN_NUM=${slot}
TUN_LOCAL_IP=${server_ip}
TUN_REMOTE_IP=${client_ip}
EOF
        chmod 600 "$conf"

        # Append authorized_keys entry tagged with vpn-user:<name> for clean removal
        touch "${AUTH_KEYS}"
        chmod 600 "${AUTH_KEYS}"
        echo "tunnel=\"${slot}\" ${pubkey} vpn-user:${username}" >> "${AUTH_KEYS}"

        log "User '${username}' added successfully:"
        log "  Tun slot  : ${slot}  (tun${slot})"
        log "  Server IP : ${server_ip}"
        log "  Client IP : ${client_ip}"
        log ""
        log "Client connect (using vpn-connect.sh):"
        log "  sudo ./vpn-connect.sh --tun ${slot}"
        log "  sudo ./vpn-connect.sh --tun ${slot} --full-tunnel"
        log ""
        log "Client connect (manual):"
        log "  sudo ssh -w ${slot}:${slot} -o Tunnel=point-to-point \\"
        log "       -i ~/.ssh/<key> -p <PORT> root@<SERVER> -f -N"
        log "  sudo ip addr add ${client_ip}/32 peer ${server_ip} dev tun${slot}"
        log "  sudo ip link set tun${slot} mtu 1340 up"
        ;;

    # ── remove ────────────────────────────────────────────────────────────────
    remove)
        [[ $# -lt 2 ]] && usage
        username="$2"
        conf="${USERS_DIR}/${username}.conf"
        [[ -f "$conf" ]] || err "User '${username}' not found"

        slot=$(grep '^TUN_NUM=' "$conf" | cut -d= -f2)

        # Remove the authorized_keys entry (line tagged vpn-user:<username>)
        if [[ -f "${AUTH_KEYS}" ]]; then
            sed -i "/vpn-user:${username}\b/d" "${AUTH_KEYS}"
        fi

        rm -f "$conf"

        # Tear down tun device if it is currently up
        ip link delete "tun${slot}" 2>/dev/null && \
            log "  tun${slot} removed" || true

        log "User '${username}' removed (tun slot ${slot} is now free)"
        ;;

    # ── list ──────────────────────────────────────────────────────────────────
    list)
        shopt -s nullglob
        confs=("${USERS_DIR}"/*.conf)
        if [[ ${#confs[@]} -eq 0 ]]; then
            log "No VPN users configured. Use 'add' to create one."
            exit 0
        fi
        printf "%-16s  %-5s  %-16s  %-16s  %s\n" \
            "USERNAME" "TUN" "SERVER_IP" "CLIENT_IP" "STATUS"
        printf "%-16s  %-5s  %-16s  %-16s  %s\n" \
            "--------" "---" "---------" "---------" "------"
        for conf in "${confs[@]}"; do
            uname=$(grep '^USERNAME='      "$conf" | cut -d= -f2)
            slot=$(grep  '^TUN_NUM='       "$conf" | cut -d= -f2)
            sip=$(grep   '^TUN_LOCAL_IP='  "$conf" | cut -d= -f2)
            cip=$(grep   '^TUN_REMOTE_IP=' "$conf" | cut -d= -f2)
            status="down"
            ip link show "tun${slot}" &>/dev/null && status="up"
            printf "%-16s  %-5s  %-16s  %-16s  %s\n" \
                "${uname}" "tun${slot}" "${sip}" "${cip}" "${status}"
        done
        ;;

    # ── show ──────────────────────────────────────────────────────────────────
    show)
        [[ $# -lt 2 ]] && usage
        username="$2"
        conf="${USERS_DIR}/${username}.conf"
        [[ -f "$conf" ]] || err "User '${username}' not found"
        cat "$conf"
        ;;

    *) usage ;;
esac
