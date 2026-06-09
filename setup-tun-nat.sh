#!/usr/bin/env bash
set -euo pipefail

# =========================
# Smart TUN NAT Setup Script
# =========================

TUN_IF="${TUN_IF:-tun0}"
TUN_NET="${TUN_NET:-10.0.0.0/30}"
LAN_NET="${LAN_NET:-192.168.50.0/24}"

# Auto-detect WAN interface
WAN_IF="${WAN_IF:-$(ip route get 8.8.8.8 | awk '{for(i=1;i<=NF;i++) if ($i=="dev") print $(i+1); exit}')}"

if [[ -z "$WAN_IF" ]]; then
  echo "ERROR: Could not detect WAN interface."
  echo "Set it manually like:"
  echo "WAN_IF=eth0 sudo ./setup-tun-nat.sh"
  exit 1
fi

echo "Using settings:"
echo "  TUN_IF  = $TUN_IF"
echo "  WAN_IF  = $WAN_IF"
echo "  TUN_NET = $TUN_NET"
echo "  LAN_NET = $LAN_NET"
echo

# Check tun interface exists
if ! ip link show "$TUN_IF" >/dev/null 2>&1; then
  echo "ERROR: Interface $TUN_IF does not exist."
  echo "Create/start your tunnel first."
  exit 1
fi

# Enable IPv4 forwarding now
echo "Enabling IPv4 forwarding..."
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null

# Enable IPv4 forwarding permanently
echo "Saving IPv4 forwarding permanently..."
echo "net.ipv4.ip_forward=1" | sudo tee /etc/sysctl.d/99-ip-forward.conf >/dev/null
sudo sysctl --system >/dev/null

# Helper: add iptables rule only if it does not already exist
add_rule() {
  local table="$1"
  shift

  if sudo iptables -t "$table" -C "$@" 2>/dev/null; then
    echo "Exists: iptables -t $table -A $*"
  else
    sudo iptables -t "$table" -A "$@"
    echo "Added:  iptables -t $table -A $*"
  fi
}

echo
echo "Adding FORWARD rules..."

# TUN to Internet/LAN
add_rule filter FORWARD -i "$TUN_IF" -o "$WAN_IF" -s "$TUN_NET" -j ACCEPT

# Internet/LAN return traffic to TUN
add_rule filter FORWARD -i "$WAN_IF" -o "$TUN_IF" -d "$TUN_NET" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

# Explicit LAN access
add_rule filter FORWARD -i "$TUN_IF" -o "$WAN_IF" -s "$TUN_NET" -d "$LAN_NET" -j ACCEPT

# LAN return traffic
add_rule filter FORWARD -i "$WAN_IF" -o "$TUN_IF" -s "$LAN_NET" -d "$TUN_NET" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

echo
echo "Adding NAT rule..."

# NAT TUN traffic through WAN/LAN interface
add_rule nat POSTROUTING -s "$TUN_NET" -o "$WAN_IF" -j MASQUERADE

echo
echo "Installing iptables-persistent if needed..."

if ! dpkg -s iptables-persistent >/dev/null 2>&1; then
  sudo DEBIAN_FRONTEND=noninteractive apt update
  sudo DEBIAN_FRONTEND=noninteractive apt install -y iptables-persistent netfilter-persistent
else
  echo "iptables-persistent already installed."
fi

echo
echo "Saving iptables rules..."

sudo mkdir -p /etc/iptables
sudo iptables-save | sudo tee /etc/iptables/rules.v4 >/dev/null

if command -v netfilter-persistent >/dev/null 2>&1; then
  sudo netfilter-persistent save
fi

echo
echo "Done."
echo
echo "Current rules:"
sudo iptables -S FORWARD
echo
sudo iptables -t nat -S POSTROUTING

# If your WAN interface is not auto-detected correctly, run it manually like:
# sudo WAN_IF=eth0 ./setup-tun-nat.sh
# Or with custom tunnel network:
# sudo TUN_IF=tun0 TUN_NET=10.0.0.0/30 LAN_NET=192.168.50.0/24 ./setup-tun-nat.sh