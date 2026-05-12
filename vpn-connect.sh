#!/bin/bash
# ─────────────────────────────────────────────────────────────────────────────
# VPN Connect — SSH Tunnel to ssh-vpn-server via public gateway
#
# Usage:  sudo ./vpn-connect.sh [--full-tunnel] [--disconnect]
#
# Routes (split tunnel by default):
#   10.10.10.0/30  → always (VPN link)
#   192.168.4.0/24 → LAN behind VPN server
#   --full-tunnel  → route ALL traffic through VPN (0.0.0.0/1 + 128.0.0.0/1)
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
GW_HOST="157.180.4.166"
GW_PORT="5500"                        # public port (NAT → 2255 inside gateway)
SSH_KEY="${HOME}/.ssh/id_circle_k8s"

# Legacy defaults (tun0 / single-user mode)
TUN_DEV="tun0"
CLIENT_TUN_IP="10.10.10.1"
SERVER_TUN_IP="10.10.10.2"
TUN_MTU="1420"
LAN_SUBNET="192.168.4.0/24"          # subnet behind VPN server — update if needed

FULL_TUNNEL=0
DISCONNECT=0
TUN_NUM=""                            # empty = legacy tun0 mode

# ── Parse args ────────────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
        --full-tunnel) FULL_TUNNEL=1 ;;
        --disconnect)  DISCONNECT=1  ;;
        --tun=*)       TUN_NUM="${arg#--tun=}" ;;
        --tun)         : ;;  # handled via shift below — see next pass
    esac
done
# Second pass for --tun N (space-separated)
args=("$@")
for (( i=0; i<${#args[@]}; i++ )); do
    if [[ "${args[$i]}" == "--tun" && $((i+1)) -lt ${#args[@]} ]]; then
        TUN_NUM="${args[$((i+1))]}"
    fi
done

# ── Resolve tun device and IPs ────────────────────────────────────────────────
if [[ -n "$TUN_NUM" && "$TUN_NUM" != "0" ]]; then
    # Multi-user mode: slot N → tun<N>, IPs 10.10.<N>.1 / 10.10.<N>.2
    if ! [[ "$TUN_NUM" =~ ^[0-9]+$ ]] || [[ "$TUN_NUM" -lt 1 || "$TUN_NUM" -gt 253 ]]; then
        echo "[ERROR] --tun must be a number between 1 and 253"
        exit 1
    fi
    TUN_DEV="tun${TUN_NUM}"
    CLIENT_TUN_IP="10.10.${TUN_NUM}.1"
    SERVER_TUN_IP="10.10.${TUN_NUM}.2"
else
    TUN_NUM=0  # legacy default
fi

# ── Disconnect mode ───────────────────────────────────────────────────────────
if [[ $DISCONNECT -eq 1 ]]; then
    echo "[*] Disconnecting VPN (${TUN_DEV})..."
    sudo ip link delete "${TUN_DEV}" 2>/dev/null && echo "[OK] ${TUN_DEV} removed" || echo "[INFO] ${TUN_DEV} not found"
    pkill -f "ssh.*-w ${TUN_NUM}:${TUN_NUM}.*${GW_HOST}" 2>/dev/null || true
    echo "[OK] Done."
    exit 0
fi

# ── Check for root ────────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This script must be run with sudo"
    exit 1
fi

# ── Remove stale tun device ───────────────────────────────────────────────────
if ip link show "${TUN_DEV}" &>/dev/null; then
    echo "[INFO] Removing existing ${TUN_DEV}..."
    ip link delete "${TUN_DEV}" 2>/dev/null || true
fi

# ── Establish tunnel ──────────────────────────────────────────────────────────
echo "[*] Connecting to ${GW_HOST}:${GW_PORT} via SSH tunnel (tun slot ${TUN_NUM})..."
ssh -i "${SSH_KEY}" \
    -p "${GW_PORT}" \
    -w "${TUN_NUM}:${TUN_NUM}" \
    -o Tunnel=point-to-point \
    -o StrictHostKeyChecking=accept-new \
    -o ServerAliveInterval=30 \
    -o ServerAliveCountMax=3 \
    -o ExitOnForwardFailure=yes \
    -f -N \
    root@"${GW_HOST}"

# ── Configure client-side tun ─────────────────────────────────────────────────
echo "[*] Waiting for ${TUN_DEV}..."
for i in $(seq 1 10); do
    ip link show "${TUN_DEV}" &>/dev/null && break
    sleep 1
done

if ! ip link show "${TUN_DEV}" &>/dev/null; then
    echo "[ERROR] ${TUN_DEV} did not appear after 10s"
    exit 1
fi

echo "[*] Configuring ${TUN_DEV}..."
ip addr add "${CLIENT_TUN_IP}/32" peer "${SERVER_TUN_IP}" dev "${TUN_DEV}"
ip link set "${TUN_DEV}" mtu "${TUN_MTU}" up

# ── Add routes ────────────────────────────────────────────────────────────────
echo "[*] Adding routes..."
ip route add "${LAN_SUBNET}" via "${SERVER_TUN_IP}" dev "${TUN_DEV}" 2>/dev/null || true

if [[ $FULL_TUNNEL -eq 1 ]]; then
    echo "[*] Full tunnel mode — routing all traffic through VPN"
    # Save default gw first
    DEFAULT_GW=$(ip route show default | awk '/default/ {print $3}' | head -1)
    ip route add "${GW_HOST}/32" via "${DEFAULT_GW}" 2>/dev/null || true
    ip route add 0.0.0.0/1 via "${SERVER_TUN_IP}" dev "${TUN_DEV}" 2>/dev/null || true
    ip route add 128.0.0.0/1 via "${SERVER_TUN_IP}" dev "${TUN_DEV}" 2>/dev/null || true
else
    echo "[*] Split tunnel mode — only LAN ${LAN_SUBNET} via VPN"
fi

# ── Test ──────────────────────────────────────────────────────────────────────
echo ""
echo "  ┌─ VPN Connected ─────────────────────────────────┐"
echo "  │  Client tun IP:  ${CLIENT_TUN_IP}             │"
echo "  │  Server tun IP:  ${SERVER_TUN_IP}             │"
echo "  │  LAN subnet:     ${LAN_SUBNET}        │"
echo "  │  Mode:           $([ $FULL_TUNNEL -eq 1 ] && echo 'Full tunnel' || echo 'Split tunnel')                    │"
echo "  └─────────────────────────────────────────────────┘"
echo ""
echo "[*] Pinging server tun (${SERVER_TUN_IP})..."
ping -c 3 -W 3 "${SERVER_TUN_IP}" || echo "[WARN] Ping failed — server tun may still be configuring, retry in 5s"
echo ""
echo "[*] To disconnect: sudo ./vpn-connect.sh --disconnect"
