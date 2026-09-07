package tunnel

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"linkthings.io/client-v2/config"
	"linkthings.io/client-v2/logging"
)

// stableAdapterName is the single, always-reused tunnel device name. There is
// no per-server adapter: switching servers reuses the same device.
const stableAdapterName = "LT-Main"

// tunModePointToPoint is OpenSSH's SSH_TUNMODE_POINTOPOINT value, sent as the
// first 4 bytes of the tun@openssh.com channel-open payload.
const tunModePointToPoint = uint32(1)

// activeTunnel holds everything needed to run and tear down one connection.
// It is entirely platform-independent: all OS-specific work happens through
// plat and dev.
type activeTunnel struct {
	plat Platform
	dev  TunDevice

	sshConn *ssh.Client
	tunCh   ssh.Channel
	tunSlot int

	lanRoute     Route
	fullTunnel   bool
	hostPinRoute Route
	splitRoutes  []Route
	dnsApplied   bool

	connectedAt time.Time
	rxBytes     atomic.Uint64
	txBytes     atomic.Uint64
	closed      atomic.Bool
}

// CleanupOrphanAdapters removes a stale tunnel device left behind by a
// crashed prior process. Called once at startup.
func CleanupOrphanAdapters() error {
	return currentPlatform().CleanupOrphans(stableAdapterName)
}

func (tm *TunnelManager) Connect(server config.ServerConfig, signer ssh.Signer) error {
	tm.mu.Lock()
	if tm.state != nil {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel is already connected")
	}
	tm.mu.Unlock()

	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid server config: %w", err)
	}
	if signer == nil {
		return fmt.Errorf("ssh signer is required")
	}

	gatewayHost := server.Gateway
	if !strings.Contains(gatewayHost, ":") {
		gatewayHost += ":22"
	}

	localIPStr, localMaskStr, err := ipv4FromCIDR(server.LocalIP)
	if err != nil {
		return fmt.Errorf("invalid localIP %q: %w", server.LocalIP, err)
	}
	lanRouteIPStr, lanMaskStr, err := networkFromCIDR(server.LANSubnet)
	if err != nil {
		return fmt.Errorf("invalid lanSubnet %q: %w", server.LANSubnet, err)
	}

	tunSlot := server.SSHTunnel
	tunMTU := server.MTU
	if tunMTU == "" {
		tunMTU = "1340"
	}
	mtu, err := strconv.Atoi(tunMTU)
	if err != nil {
		return fmt.Errorf("invalid mtu %q: %w", tunMTU, err)
	}

	plat := currentPlatform()
	logging.Infof("tunnel_connect_start adapter=%s gateway=%s tunnel=%d mtu=%s", stableAdapterName, gatewayHost, tunSlot, tunMTU)

	dev, err := plat.OpenOrCreateTun(stableAdapterName, mtu)
	if err != nil {
		return fmt.Errorf("open/create tunnel device failed: %w", err)
	}

	localIP := net.ParseIP(localIPStr)
	localMask := net.IPMask(net.ParseIP(localMaskStr).To4())
	if err := plat.ConfigureAddress(dev, localIP, localMask); err != nil {
		return fmt.Errorf("configure tunnel address failed: %w", err)
	}

	dnsApplied := false
	if dnsStrs := parseDNSServers(server.DNS); len(dnsStrs) > 0 {
		var dnsServers []net.IP
		for _, s := range dnsStrs {
			if ip := net.ParseIP(s); ip != nil {
				dnsServers = append(dnsServers, ip)
			}
		}
		if len(dnsServers) > 0 {
			if err := plat.SetDNS(dev, dnsServers); err != nil {
				logging.Errorf("set_dns_failed err=%v", err)
			} else {
				dnsApplied = true
				logging.Infof("set_dns_ok dns=%v", dnsServers)
			}
		}
	}

	sshCfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", gatewayHost, sshCfg)
	if err != nil {
		// Keep the tunnel device alive for reuse; auth/network failures
		// should not churn the device's identity.
		_ = dev.Close()
		return fmt.Errorf("ssh dial failed: %w", err)
	}

	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], tunModePointToPoint)
	binary.BigEndian.PutUint32(payload[4:8], uint32(tunSlot))

	tunCh, reqs, err := sshConn.OpenChannel("tun@openssh.com", payload)
	if err != nil {
		_ = sshConn.Close()
		_ = dev.Close()
		return fmt.Errorf("open tun channel failed: %w", err)
	}
	go ssh.DiscardRequests(reqs)

	serverTunDev := fmt.Sprintf("tun%d", tunSlot)
	serverCfgCmd := fmt.Sprintf(
		"sysctl -w net.ipv4.ip_forward=1 2>/dev/null; "+
			"iptables -t filter -A FORWARD -i %s -j ACCEPT 2>/dev/null; "+
			"iptables -t filter -A FORWARD -o %s -j ACCEPT 2>/dev/null; "+
			"ip link set %s mtu %s 2>/dev/null; "+
			"ip addr flush dev %s 2>/dev/null; "+
			"ip addr add %s/32 peer %s dev %s; "+
			"ip link set %s up",
		serverTunDev,
		serverTunDev,
		serverTunDev, tunMTU,
		serverTunDev,
		server.RemoteIP, localIPStr, serverTunDev,
		serverTunDev,
	)
	cfgSess, err := sshConn.NewSession()
	if err == nil {
		out, runErr := cfgSess.CombinedOutput(serverCfgCmd)
		_ = cfgSess.Close()
		if runErr != nil {
			logging.Errorf("server_tun_config_failed err=%v out=%s", runErr, strings.TrimSpace(string(out)))
		}
	} else {
		logging.Errorf("server_tun_session_failed err=%v", err)
	}

	lanRoute := Route{
		Dest:      net.IPNet{IP: net.ParseIP(lanRouteIPStr), Mask: net.IPMask(net.ParseIP(lanMaskStr).To4())},
		Gateway:   net.ParseIP(server.RemoteIP),
		ViaDevice: dev,
	}
	if err := plat.AddRoute(lanRoute); err != nil {
		logging.Errorf("route_add_failed route=%s mask=%s gw=%s err=%v", lanRouteIPStr, lanMaskStr, server.RemoteIP, err)
	}

	at := &activeTunnel{
		plat:        plat,
		dev:         dev,
		sshConn:     sshConn,
		tunCh:       tunCh,
		tunSlot:     tunSlot,
		lanRoute:    lanRoute,
		fullTunnel:  server.FullTunnel,
		dnsApplied:  dnsApplied,
		connectedAt: time.Now(),
	}
	at.closed.Store(false)

	if at.fullTunnel {
		gw, err := plat.DefaultGateway()
		if err != nil {
			logging.Errorf("full_tunnel: cannot detect default gateway: %v", err)
		} else {
			logging.Infof("full_tunnel: enabled, original_gw=%s", gw)

			sshHostIP := strings.Split(gatewayHost, ":")[0]
			hostPin := Route{
				Dest:    net.IPNet{IP: net.ParseIP(sshHostIP), Mask: net.CIDRMask(32, 32)},
				Gateway: gw,
			}
			if err := plat.AddRoute(hostPin); err != nil {
				logging.Errorf("full_tunnel: failed to pin ssh host route host=%s gw=%s err=%v", sshHostIP, gw, err)
			}

			remoteIP := net.ParseIP(server.RemoteIP)
			lower := Route{Dest: net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(1, 32)}, Gateway: remoteIP, ViaDevice: dev}
			upper := Route{Dest: net.IPNet{IP: net.IPv4(128, 0, 0, 0), Mask: net.CIDRMask(1, 32)}, Gateway: remoteIP, ViaDevice: dev}
			if err := plat.AddRoute(lower); err != nil {
				logging.Errorf("full_tunnel: failed to set lower /1 route via %s err=%v", server.RemoteIP, err)
			}
			if err := plat.AddRoute(upper); err != nil {
				logging.Errorf("full_tunnel: failed to set upper /1 route via %s err=%v", server.RemoteIP, err)
			}

			at.hostPinRoute = hostPin
			at.splitRoutes = []Route{lower, upper}
			logging.Infof("full_tunnel: all internet routed via %s (SSH host %s via original gateway)", server.RemoteIP, sshHostIP)
		}
	}

	at.startBridge()

	tm.mu.Lock()
	tm.state = at
	tm.mu.Unlock()

	logging.Infof("tunnel_connect_ok adapter=%s route=%s/%s", stableAdapterName, lanRouteIPStr, lanMaskStr)
	return nil
}

func (tm *TunnelManager) Disconnect() error {
	tm.mu.Lock()
	state := tm.state
	tm.state = nil
	tm.mu.Unlock()

	if state == nil {
		return nil
	}

	at, ok := state.(*activeTunnel)
	if !ok {
		return fmt.Errorf("invalid tunnel state")
	}

	at.close()
	logging.Infof("tunnel_disconnect_ok adapter=%s", stableAdapterName)
	return nil
}

func (at *activeTunnel) close() {
	if at.closed.Swap(true) {
		return
	}

	if at.tunCh != nil {
		_ = at.tunCh.Close()
	}

	// Clean up server-side iptables rules before closing SSH.
	if at.sshConn != nil {
		serverTunDev := fmt.Sprintf("tun%d", at.tunSlot)
		iptablesCleanup := fmt.Sprintf(
			"iptables -t filter -D FORWARD -i %s -j ACCEPT 2>/dev/null; "+
				"iptables -t filter -D FORWARD -o %s -j ACCEPT 2>/dev/null",
			serverTunDev,
			serverTunDev,
		)
		if cleanupSess, err := at.sshConn.NewSession(); err == nil {
			_ = cleanupSess.Run(iptablesCleanup)
			_ = cleanupSess.Close()
		}
		_ = at.sshConn.Close()
	}

	if err := at.plat.DeleteRoute(at.lanRoute); err != nil {
		logging.Errorf("route_delete_failed err=%v", err)
	}

	// Full-tunnel teardown removes the two /1 routes but deliberately leaves
	// the SSH-host pin route in place, matching documented Windows semantics.
	if at.fullTunnel && len(at.splitRoutes) == 2 {
		logging.Infof("full_tunnel: cleaning up routes")
		for _, r := range at.splitRoutes {
			_ = at.plat.DeleteRoute(r)
		}
	}

	if at.dnsApplied {
		_ = at.plat.RevertDNS(at.dev)
	}

	// End the device's packet-exchange session, but do not destroy the
	// underlying OS network interface - it stays alive for reuse on the
	// next connect.
	_ = at.dev.Close()
}

func (at *activeTunnel) stats() Stats {
	return Stats{
		Connected:   true,
		ConnectedAt: at.connectedAt,
		RxBytes:     at.rxBytes.Load(),
		TxBytes:     at.txBytes.Load(),
	}
}
