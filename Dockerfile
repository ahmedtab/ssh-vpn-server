# ─────────────────────────────────────────────────────────────────────────────
# SSH VPN Tunnel Server
# Image: registry.app.circle360.net/ssh-vpn-tunnel-server:v1.0
#
# Architecture:
#   VPN Client (tun0/tun100)
#     └─> Public SSH Server (port-forward / NAT)
#           └─> [This Container] SSH VPN Server (tun device + NAT)
#                 └─> Internet / LAN / Resources
#
# Requires on docker run:
#   --cap-add NET_ADMIN --cap-add NET_RAW
#   --device /dev/net/tun:/dev/net/tun
#   --sysctl net.ipv4.ip_forward=1
# ─────────────────────────────────────────────────────────────────────────────

FROM ubuntu:24.04

LABEL org.opencontainers.image.title="SSH VPN Tunnel Server"
LABEL org.opencontainers.image.description="SSH-based VPN server with tun tunneling. Supports nm-ssh (NetworkManager) and raw SSH -w tunnel clients including Windows/WSL2."
LABEL org.opencontainers.image.version="1.0"
LABEL org.opencontainers.image.source="https://registry.app.circle360.net"

ENV DEBIAN_FRONTEND=noninteractive

# ── Install packages ──────────────────────────────────────────────────────────
# Ubuntu 24.04 chosen for: LTS stability, broad apt ecosystem, Ubuntu-familiar
RUN apt-get update && apt-get install -y --no-install-recommends \
    # SSH Server
    openssh-server \
    # Network routing & interfaces
    iproute2 \
    iputils-ping \
    iputils-tracepath \
    traceroute \
    # Firewall / NAT
    iptables \
    iptables-persistent \
    netfilter-persistent \
    # Network diagnostics
    net-tools \
    ncat \
    tcpdump \
    nmap \
    dnsutils \
    # System
    procps \
    psmisc \
    kmod \
    # Monitoring & debug
    htop \
    less \
    # Editors
    nano \
    vim-tiny \
    # Download tools
    curl \
    wget \
    ca-certificates \
    # Gateway reverse tunnel
    autossh \
    sshpass \
    # Cron (fallback tun setup, optional)
    cron \
    && rm -rf /var/lib/apt/lists/*

# ── Use iptables-legacy for Docker compatibility ───────────────────────────────
# nftables backend can conflict with Docker's iptables rules
RUN update-alternatives --set iptables  /usr/sbin/iptables-legacy  2>/dev/null || true && \
    update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy 2>/dev/null || true

# ── Directory structure ────────────────────────────────────────────────────────
RUN mkdir -p \
    /run/sshd \
    /root/.ssh \
    /etc/ssh/server_keys \
    /etc/vpn/users \
    /var/log/vpn

RUN chmod 700 /root/.ssh

# ── Copy scripts ───────────────────────────────────────────────────────────────
COPY scripts/entrypoint.sh      /entrypoint.sh
COPY scripts/tun-monitor.sh    /usr/local/bin/tun-monitor.sh
COPY scripts/gateway-tunnel.sh /usr/local/bin/gateway-tunnel.sh
COPY scripts/manage-users.sh   /usr/local/bin/manage-users.sh

RUN chmod +x /entrypoint.sh /usr/local/bin/tun-monitor.sh /usr/local/bin/gateway-tunnel.sh /usr/local/bin/manage-users.sh

# ─────────────────────────────────────────────────────────────────────────────
# Environment Variables (all overridable at runtime)
# ─────────────────────────────────────────────────────────────────────────────

# SSH
ENV SSH_PORT=2222

# Tunnel device:
#   tun0   → for raw SSH (-w 0:0) clients / manual vpn-connect.sh style
#   tun100 → for NetworkManager SSH (nm-ssh) clients (default nm-ssh value)
ENV TUN_DEV=tun0

# Tunnel IPs (point-to-point /32)
#   TUN_LOCAL_IP  = this server's tun interface IP
#   TUN_REMOTE_IP = client's tun interface IP (peer)
ENV TUN_LOCAL_IP=10.10.10.2
ENV TUN_REMOTE_IP=10.10.10.1
ENV TUN_NETMASK=255.255.255.252

# MTU: lower than 1500 to account for SSH overhead
ENV TUN_MTU=1340

# Routing
ENV LAN_SUBNET=192.168.0.0/24
ENV LAN_IFACE=""

# NAT - MASQUERADE VPN client traffic onto LAN interface
# NOTE: split vs full tunnel is a CLIENT-side routing decision.
#   The server always NATs all traffic arriving on the tun interface.
#   Client controls it by which routes it adds after connecting.
ENV ENABLE_NAT=yes

# Authentication
# SSH public key(s) for clients - newline-separated, auto-gets tunnel="N" prefix
ENV SSH_AUTHORIZED_KEYS=""
# Allow password auth (INSECURE - only for initial testing)
ENV ALLOW_PASSWORD_AUTH=no
# Root password (only effective when ALLOW_PASSWORD_AUTH=yes)
ENV SSH_ROOT_PASSWORD=""

# Logging
ENV LOG_LEVEL=INFO
ENV TUN_MONITOR_INTERVAL=5

# ── Gateway / Reverse Tunnel ─────────────────────────────────────────────────
# Automatically forwards the container SSH port to a public SSH gateway,
# so VPN clients can connect through: gateway-public-IP:GW_EXPOSED_PORT
#
# Topology:
#   VPN Client → gateway-public-ip:GW_EXPOSED_PORT
#                  └─(reverse tunnel)─► this container:SSH_PORT

# Set to "yes" to enable the gateway reverse tunnel on startup
ENV GW_ENABLED=no

# Public SSH gateway host (IP or hostname)
ENV GW_HOST=""

# SSH port on the public gateway (default 22)
ENV GW_PORT=22

# SSH user on the public gateway
ENV GW_USER=root

# Authentication — provide ONE of the following:
#
#   GW_SSH_KEY_FILE  path to mounted private key file (preferred)
#                    mount with: -v /path/to/key:/etc/ssh/gw_key:ro
#
#   GW_SSH_KEY       raw PEM key content as env var
#                    (use \\n for newlines, or base64-encoded — set
#                    GW_SSH_KEY_B64=yes when base64-encoded)
#
#   GW_PASSWORD      password auth via sshpass (less secure, testing only)
ENV GW_SSH_KEY_FILE=""
ENV GW_SSH_KEY=""
ENV GW_SSH_KEY_B64=no
ENV GW_PASSWORD=""

# Port on the public gateway where the reverse tunnel binds (internal)
ENV GW_EXPOSED_PORT=2255
# Public port clients connect to (if NAT/firewall maps a different port → GW_EXPOSED_PORT)
# Defaults to GW_EXPOSED_PORT when not set
ENV GW_PUBLIC_PORT=""

# Bind address for the exposed port on the gateway.
# 0.0.0.0 = accessible externally (requires GatewayPorts yes on gateway sshd)
# 127.0.0.1 = only from gateway localhost
ENV GW_BIND_ADDR=0.0.0.0

# Seconds between reconnect attempts when the gateway tunnel drops
ENV GW_RECONNECT_INTERVAL=30

# ─────────────────────────────────────────────────────────────────────────────
EXPOSE 2222

HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
    CMD pgrep -x sshd > /dev/null || exit 1

ENTRYPOINT ["/entrypoint.sh"]
