#!/bin/bash
# SSH VPN Setup Script
# Configures SSH tunnel VPN with NetworkManager (nm-ssh) on Ubuntu client
# and auto-configures the remote LXC/server side

set -e

# ─── Colors ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[INFO]${RESET} $*"; }
success() { echo -e "${GREEN}[OK]${RESET}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET} $*"; }
error()   { echo -e "${RED}[ERROR]${RESET} $*"; }
section() { echo -e "\n${BOLD}━━━ $* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"; }

ask() {
    local prompt="$1" default="$2" var="$3"
    if [[ -n "$default" ]]; then
        echo -ne "${BOLD}  ${prompt}${RESET} ${YELLOW}[${default}]${RESET}: "
    else
        echo -ne "${BOLD}  ${prompt}${RESET}: "
    fi
    read -r input
    eval "$var='${input:-$default}'"
}

ask_secret() {
    local prompt="$1" var="$2"
    echo -ne "${BOLD}  ${prompt}${RESET}: "
    read -r input
    eval "$var='$input'"
}

# ─── Header ───────────────────────────────────────────────────────────────────
clear
echo -e "${BOLD}${CYAN}"
echo "  ╔══════════════════════════════════════════════╗"
echo "  ║         SSH VPN Setup Wizard                 ║"
echo "  ║   NetworkManager SSH Tunnel Configurator     ║"
echo "  ╚══════════════════════════════════════════════╝"
echo -e "${RESET}"
echo "  This script will configure an SSH tunnel VPN between"
echo "  your Ubuntu client and a remote Linux server/LXC."
echo ""

# ─── Section 1: Connection Info ───────────────────────────────────────────────
section "1. Connection Details"

ask "VPN connection name" "ssh-vpn" VPN_NAME
ask "Remote server public IP or hostname" "" REMOTE_HOST
[[ -z "$REMOTE_HOST" ]] && { error "Remote host is required."; exit 1; }

ask "SSH port on remote server (exposed via NAT/firewall)" "2255" REMOTE_PORT
ask "SSH private key file path" "$HOME/.ssh/id_ed25519" SSH_KEY

if [[ ! -f "$SSH_KEY" ]]; then
    warn "Key file not found: $SSH_KEY"
    echo -ne "  ${YELLOW}List available keys:${RESET} "
    ls ~/.ssh/id_* 2>/dev/null | grep -v '\.pub' | tr '\n' '  ' && echo ""
    ask "SSH private key file path" "" SSH_KEY
    [[ ! -f "$SSH_KEY" ]] && { error "Key file not found. Aborting."; exit 1; }
fi

ask "SSH user on remote server" "root" SSH_USER

# ─── Section 2: Tunnel IPs ────────────────────────────────────────────────────
section "2. Tunnel IP Configuration"
echo "  These IPs are used ONLY inside the tunnel (not your LAN)."
echo "  Use a /30 subnet (4 IPs, 2 usable) e.g. 10.10.10.0/30"
echo ""

ask "Client tunnel IP (your side)" "10.10.10.1" TUN_LOCAL_IP
ask "Server tunnel IP (remote side)" "10.10.10.2" TUN_REMOTE_IP
ask "Tunnel netmask" "255.255.255.252" TUN_NETMASK
ask "Tunnel MTU (recommended: 1340 for SSH overhead)" "1340" TUN_MTU

# ─── Section 3: LAN Routing ───────────────────────────────────────────────────
section "3. LAN Routing"
echo "  Subnets you want to access through the VPN (your remote LAN)."
echo "  Example: 192.168.4.0/24"
echo ""

ask "Remote LAN subnet (CIDR)" "192.168.4.0/24" LAN_SUBNET

# Extract gateway suggestion from subnet
LAN_BASE=$(echo "$LAN_SUBNET" | cut -d'/' -f1 | sed 's/\.[0-9]*$//')
ask "Remote LAN gateway IP" "${LAN_BASE}.1" LAN_GW

ask "Route all internet traffic through VPN? (yes/no)" "no" FULL_TUNNEL

# ─── Section 4: Remote Server Config ─────────────────────────────────────────
section "4. Remote Server Configuration"
echo "  The script will SSH into the remote server to configure:"
echo "  - /dev/net/tun availability"
echo "  - authorized_keys tunnel permission"
echo "  - tun interface auto-setup script"
echo "  - iptables MASQUERADE (NAT)"
echo "  - ip_forward"
echo ""

ask "Configure remote server automatically? (yes/no)" "yes" AUTO_REMOTE

# ─── Summary Before Applying ──────────────────────────────────────────────────
section "Configuration Summary"
echo ""
echo -e "  ${BOLD}VPN Name:${RESET}        $VPN_NAME"
echo -e "  ${BOLD}Remote Host:${RESET}     $REMOTE_HOST:$REMOTE_PORT"
echo -e "  ${BOLD}SSH Key:${RESET}         $SSH_KEY"
echo -e "  ${BOLD}SSH User:${RESET}        $SSH_USER"
echo -e "  ${BOLD}Client tun IP:${RESET}   $TUN_LOCAL_IP"
echo -e "  ${BOLD}Server tun IP:${RESET}   $TUN_REMOTE_IP"
echo -e "  ${BOLD}Tunnel netmask:${RESET}  $TUN_NETMASK"
echo -e "  ${BOLD}Tunnel MTU:${RESET}      $TUN_MTU"
echo -e "  ${BOLD}LAN Subnet:${RESET}      $LAN_SUBNET"
echo -e "  ${BOLD}LAN Gateway:${RESET}     $LAN_GW"
echo -e "  ${BOLD}Full tunnel:${RESET}     $FULL_TUNNEL"
echo -e "  ${BOLD}Auto-configure remote:${RESET} $AUTO_REMOTE"
echo ""

echo -ne "${YELLOW}  Proceed with this configuration? (yes/no) [yes]: ${RESET}"
read -r confirm
confirm="${confirm:-yes}"
[[ "$confirm" != "yes" ]] && { info "Aborted."; exit 0; }

# ─── Step 1: Check Prerequisites ─────────────────────────────────────────────
section "Step 1: Checking Prerequisites"

# Check nm-ssh plugin installed
if ! dpkg -l network-manager-ssh &>/dev/null; then
    info "Installing network-manager-ssh..."
    sudo apt-get install -y network-manager-ssh
    success "network-manager-ssh installed"
else
    success "network-manager-ssh is installed"
fi

# Check SSH key permissions
KEY_PERMS=$(stat -c "%a" "$SSH_KEY")
if [[ "$KEY_PERMS" != "600" && "$KEY_PERMS" != "400" ]]; then
    warn "SSH key permissions are $KEY_PERMS, fixing to 600..."
    chmod 600 "$SSH_KEY"
fi
success "SSH key permissions OK"

# Test SSH connectivity
info "Testing SSH connection to $REMOTE_HOST:$REMOTE_PORT ..."
if ! ssh -i "$SSH_KEY" -p "$REMOTE_PORT" -o ConnectTimeout=10 \
    -o StrictHostKeyChecking=accept-new \
    -o BatchMode=yes \
    "${SSH_USER}@${REMOTE_HOST}" "echo connected" &>/dev/null; then
    error "Cannot connect to ${SSH_USER}@${REMOTE_HOST}:${REMOTE_PORT}"
    error "Check host, port, and key then re-run."
    exit 1
fi
success "SSH connection to $REMOTE_HOST:$REMOTE_PORT OK"

# ─── Step 2: Configure Remote Server ─────────────────────────────────────────
if [[ "$AUTO_REMOTE" == "yes" ]]; then
    section "Step 2: Configuring Remote Server"

    # Detect which tun device NM-SSH uses (tun100 by default)
    NM_TUN_DEV="tun100"

    ssh -i "$SSH_KEY" -p "$REMOTE_PORT" "${SSH_USER}@${REMOTE_HOST}" "
set -e

# Check /dev/net/tun
if [[ ! -c /dev/net/tun ]]; then
    echo 'ERROR: /dev/net/tun missing - add tun device to LXC config on Proxmox host'
    exit 1
fi
echo 'TUN device: OK'

# Fix authorized_keys - allow tunnel=100
if grep -q 'tunnel=\"0\"' /root/.ssh/authorized_keys; then
    sed -i 's/tunnel=\"0\"/tunnel=\"100\"/' /root/.ssh/authorized_keys
    echo 'authorized_keys: updated tunnel=0 to tunnel=100'
elif ! grep -q 'tunnel=' /root/.ssh/authorized_keys; then
    echo 'authorized_keys: no tunnel restriction found - OK'
else
    echo 'authorized_keys: tunnel permission already set'
fi

# Enable ip_forward permanently
grep -q 'net.ipv4.ip_forward=1' /etc/sysctl.conf || \
    echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
sysctl -w net.ipv4.ip_forward=1 > /dev/null
echo 'IP forwarding: enabled'

# Create tun auto-setup script
cat > /usr/local/bin/tun-vpn-setup.sh << 'SCRIPT'
#!/bin/bash
TUN_DEV=\$(ip link show | grep -o 'tun[0-9]*' | head -1)
[[ -z \"\$TUN_DEV\" ]] && exit 0
ip addr show \"\$TUN_DEV\" | grep -q 'inet' && exit 0
ip addr add ${TUN_REMOTE_IP} peer ${TUN_LOCAL_IP}/32 dev \"\$TUN_DEV\" 2>/dev/null || true
ip link set \"\$TUN_DEV\" mtu ${TUN_MTU}
ip link set \"\$TUN_DEV\" up
sysctl -w net.ipv4.ip_forward=1 > /dev/null
SCRIPT
chmod +x /usr/local/bin/tun-vpn-setup.sh
echo 'tun-vpn-setup.sh: created'

# Cron job to run setup script every minute when tun exists
echo '* * * * * root /usr/local/bin/tun-vpn-setup.sh' > /etc/cron.d/tun-vpn-setup
chmod 644 /etc/cron.d/tun-vpn-setup
echo 'cron job: installed'

# iptables MASQUERADE
LAN_IFACE=\$(ip route | grep 'default' | awk '{print \$5}' | head -1)
iptables -t nat -C POSTROUTING -o \"\$LAN_IFACE\" -j MASQUERADE 2>/dev/null || \
    iptables -t nat -A POSTROUTING -o \"\$LAN_IFACE\" -j MASQUERADE
iptables -C FORWARD -i 'tun+' -o \"\$LAN_IFACE\" -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -i 'tun+' -o \"\$LAN_IFACE\" -j ACCEPT
iptables -C FORWARD -i \"\$LAN_IFACE\" -o 'tun+' -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -i \"\$LAN_IFACE\" -o 'tun+' -m state --state RELATED,ESTABLISHED -j ACCEPT
echo 'iptables MASQUERADE: configured on '\$LAN_IFACE

# Save iptables
if command -v netfilter-persistent &>/dev/null; then
    netfilter-persistent save > /dev/null
    echo 'iptables: saved persistently'
else
    apt-get install -y iptables-persistent -q < /dev/null
    netfilter-persistent save > /dev/null
    echo 'iptables-persistent: installed and saved'
fi

echo 'Remote server configuration: DONE'
"
    success "Remote server configured"
else
    section "Step 2: Skipping Remote Configuration"
    warn "Remember to manually configure remote server."
fi

# ─── Step 3: Configure NetworkManager ────────────────────────────────────────
section "Step 3: Configuring NetworkManager VPN"

# Remove existing connection if present
if nmcli con show "$VPN_NAME" &>/dev/null; then
    warn "Existing connection '$VPN_NAME' found, removing..."
    nmcli con delete "$VPN_NAME"
fi

# Build VPN data string
VPN_DATA="remote=${REMOTE_HOST}, port=${REMOTE_PORT}, tunnel-mtu=${TUN_MTU}, local-ip=${TUN_LOCAL_IP}, remote-ip=${TUN_REMOTE_IP}, netmask=${TUN_NETMASK}, key-file=${SSH_KEY}"

if [[ "$SSH_USER" != "root" ]]; then
    VPN_DATA="${VPN_DATA}, remote-username=${SSH_USER}"
fi

# Create NM connection
nmcli con add \
    type vpn \
    con-name "$VPN_NAME" \
    vpn-type ssh \
    -- \
    vpn.data "$VPN_DATA" \
    ipv4.method auto \
    ipv6.method ignore

# Split tunnel (no default route via VPN)
if [[ "$FULL_TUNNEL" != "yes" ]]; then
    nmcli con modify "$VPN_NAME" ipv4.never-default yes
    success "Split tunnel enabled (internet uses local connection)"
else
    warn "Full tunnel enabled (all traffic through VPN)"
fi

# Add LAN route
nmcli con modify "$VPN_NAME" \
    +ipv4.routes "${LAN_SUBNET} ${TUN_REMOTE_IP}"

success "NetworkManager VPN connection '$VPN_NAME' created"

# ─── Step 4: Verification ────────────────────────────────────────────────────
section "Step 4: Verifying Remote Server"

info "Checking remote server config..."
REMOTE_CHECK=$(ssh -i "$SSH_KEY" -p "$REMOTE_PORT" "${SSH_USER}@${REMOTE_HOST}" "
echo '--- TUN device ---'
ls -la /dev/net/tun 2>/dev/null || echo 'MISSING'
echo '--- authorized_keys tunnel ---'
grep 'tunnel=' /root/.ssh/authorized_keys || echo 'no tunnel restriction'
echo '--- IP forward ---'
cat /proc/sys/net/ipv4/ip_forward
echo '--- iptables MASQUERADE ---'
iptables -t nat -L POSTROUTING -n | grep MASQUERADE || echo 'not set'
echo '--- tun-vpn-setup.sh ---'
cat /usr/local/bin/tun-vpn-setup.sh 2>/dev/null || echo 'not found'
")

echo "$REMOTE_CHECK"

# ─── Final Summary ────────────────────────────────────────────────────────────
section "Setup Complete"
echo ""
echo -e "${BOLD}${GREEN}  Client Configuration:${RESET}"
echo -e "  ${BOLD}VPN Name:${RESET}          $VPN_NAME"
echo -e "  ${BOLD}Connect command:${RESET}   nmcli con up $VPN_NAME"
echo -e "  ${BOLD}Disconnect:${RESET}        nmcli con down $VPN_NAME"
echo -e "  ${BOLD}Client tun IP:${RESET}     $TUN_LOCAL_IP"
echo -e "  ${BOLD}Server tun IP:${RESET}     $TUN_REMOTE_IP"
echo -e "  ${BOLD}MTU:${RESET}               $TUN_MTU"
echo ""
echo -e "${BOLD}${GREEN}  Routing:${RESET}"
echo -e "  ${BOLD}LAN subnet:${RESET}        $LAN_SUBNET  →  via $TUN_REMOTE_IP"
if [[ "$FULL_TUNNEL" == "yes" ]]; then
echo -e "  ${BOLD}Internet:${RESET}          via VPN (full tunnel)"
else
echo -e "  ${BOLD}Internet:${RESET}          direct (split tunnel)"
fi
echo ""
echo -e "${BOLD}${GREEN}  After connecting, test with:${RESET}"
echo -e "  ping -c 3 $TUN_REMOTE_IP       # tunnel"
echo -e "  ping -c 3 $LAN_GW              # LAN gateway"
echo ""
echo -e "${BOLD}${YELLOW}  Notes:${RESET}"
echo -e "  - If ping fails after connecting, wait ~60s for cron to set server tun IP"
echo -e "  - To reconnect after reboot: nmcli con up $VPN_NAME"
echo -e "  - Server-side tun IP is set by: /usr/local/bin/tun-vpn-setup.sh"
echo ""
echo -ne "${BOLD}  Connect now? (yes/no) [yes]: ${RESET}"
read -r connect_now
connect_now="${connect_now:-yes}"

if [[ "$connect_now" == "yes" ]]; then
    info "Connecting to $VPN_NAME ..."
    nmcli con up "$VPN_NAME" && success "VPN Connected!" || error "Connection failed. Check journalctl -xe for details."
fi
