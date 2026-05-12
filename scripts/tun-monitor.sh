#!/bin/bash
# ─────────────────────────────────────────────────────────────────────────────
# Tun Interface Monitor — multi-user
#
# Dynamically watches ALL tun devices defined in /etc/vpn/users/*.conf and
# configures each one (IP, MTU, up) when an SSH client establishes a tunnel.
# New user configs are picked up without a restart.
# Re-configures any tun that reappears after a client disconnect.
# ─────────────────────────────────────────────────────────────────────────────

USERS_DIR="/etc/vpn/users"
TUN_MTU="${TUN_MTU:-1340}"
INTERVAL="${TUN_MONITOR_INTERVAL:-5}"

log() { echo "[tun-monitor] $(date '+%Y-%m-%d %H:%M:%S') $*"; }

log "Started — multi-user mode, watching ${USERS_DIR} every ${INTERVAL}s"

# Per-tun state: 0 = not yet seen up, 1 = currently up
declare -A was_up

# ── Configure a single tun device ────────────────────────────────────────────
configure_tun() {
    local tun_dev="$1" local_ip="$2" remote_ip="$3" username="$4"

    log "${tun_dev} appeared (user: ${username}) — configuring..."

    if ip addr add "${local_ip}/32" peer "${remote_ip}" dev "${tun_dev}" 2>/dev/null; then
        log "  IP set: ${local_ip} <-> ${remote_ip}"
    else
        log "  WARN: IP assignment failed (may already be set)"
    fi

    ip link set "${tun_dev}" mtu "${TUN_MTU}" 2>/dev/null && log "  MTU = ${TUN_MTU}"
    ip link set "${tun_dev}" up 2>/dev/null            && log "  ${tun_dev} UP"

    # Re-enforce IP forwarding (sysctl; /proc/sys is read-only inside Docker)
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1 || true

    local addr_info
    addr_info=$(ip addr show "${tun_dev}" 2>/dev/null | grep 'inet ' || echo "unknown")
    log "  ${tun_dev} ready: ${addr_info}"
}

# ── Main loop ─────────────────────────────────────────────────────────────────
while true; do

    # Build map of currently configured users: tunN -> "local_ip:remote_ip:username"
    declare -A user_map
    shopt -s nullglob
    for conf in "${USERS_DIR}"/*.conf; do
        [[ -f "$conf" ]] || continue
        u=$(grep '^USERNAME='      "$conf" | cut -d= -f2)
        n=$(grep '^TUN_NUM='       "$conf" | cut -d= -f2)
        l=$(grep '^TUN_LOCAL_IP='  "$conf" | cut -d= -f2)
        r=$(grep '^TUN_REMOTE_IP=' "$conf" | cut -d= -f2)
        [[ -n "${n:-}" && -n "${l:-}" && -n "${r:-}" ]] || continue
        user_map["tun${n}"]="${l}:${r}:${u}"
    done

    # Check state for every known tun device
    for tun_dev in "${!user_map[@]}"; do
        IFS=: read -r local_ip remote_ip username <<< "${user_map[$tun_dev]}"

        if ip link show "${tun_dev}" &>/dev/null; then
            if ! ip addr show "${tun_dev}" 2>/dev/null | grep -q 'inet '; then
                # Device exists but has no IP yet — configure it
                configure_tun "$tun_dev" "$local_ip" "$remote_ip" "$username"
                was_up["$tun_dev"]=1
            elif [[ "${was_up[$tun_dev]:-0}" -eq 0 ]]; then
                # Already had an IP when we first noticed it
                addr_info=$(ip addr show "${tun_dev}" 2>/dev/null | grep 'inet ' | xargs || echo "?")
                log "${tun_dev} already configured (user: ${username}): ${addr_info}"
                was_up["$tun_dev"]=1
            fi
        else
            if [[ "${was_up[$tun_dev]:-0}" -eq 1 ]]; then
                log "${tun_dev} gone (user: ${username}) — waiting for reconnect..."
                was_up["$tun_dev"]=0
            fi
        fi
    done

    # Reset map for next iteration (bash associative arrays must be re-declared)
    unset user_map
    declare -A user_map

    sleep "${INTERVAL}"
done
