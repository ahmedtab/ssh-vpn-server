//go:build windows

package tunnel

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"linkthings.io/client/config"
	"linkthings.io/client/logging"
)

const (
	afInet    = uint32(2)
	afInet6   = uint32(10)
	ringBytes = 0x400000
)

const tunModePointToPoint = uint32(1)

const adapterTunnelType = "LinkThings"
const stableAdapterName = "LT-Main"

var (
	adapterCacheMu sync.Mutex
	adapterCache   = map[string]*wintun.Adapter{}
)

type activeTunnel struct {
	adapter     *wintun.Adapter
	session     wintun.Session
	sshConn     *ssh.Client
	tunCh       ssh.Channel
	adapterName string
	tunSlot     int
	lanRouteIP  string
	lanMask     string
	lanGateway  string
	fullTunnel  bool
	originalGW  string // for full-tunnel cleanup
	connectedAt time.Time
	rxBytes     atomic.Uint64
	txBytes     atomic.Uint64
	closed      atomic.Bool
}

// CleanupOrphanAdapters removes stale LT-* Wintun adapters left behind by crashes.
func CleanupOrphanAdapters() error {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"$adapters = Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue | Where-Object { (($_.InterfaceDescription -like '*Wintun*') -or ($_.DriverDescription -like '*Wintun*')) -and (($_.Name -like 'LT-*') -or (($_.Name -like 'Local Area Connection*') -and ($_.Status -eq 'Disconnected'))) }; if ($adapters) { $adapters | Remove-NetAdapter -Confirm:$false -ErrorAction Stop }",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("cleanup orphan adapters failed: %s", text)
	}
	return nil
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

	localIP, localMask, err := ipv4FromCIDR(server.LocalIP)
	if err != nil {
		return fmt.Errorf("invalid localIP %q: %w", server.LocalIP, err)
	}
	lanRouteIP, lanMask, err := networkFromCIDR(server.LANSubnet)
	if err != nil {
		return fmt.Errorf("invalid lanSubnet %q: %w", server.LANSubnet, err)
	}

	// Use server-specified tunnel slot and MTU (with defaults)
	tunSlot := server.SSHTunnel
	tunMTU := server.MTU
	if tunMTU == "" {
		tunMTU = "1340"
	}

	adapterName := stableAdapterName
	logging.Infof("tunnel_connect_start adapter=%s gateway=%s tunnel=%d mtu=%s", adapterName, gatewayHost, tunSlot, tunMTU)

	adapter, err := getOrCreateAdapter(adapterName)
	if err != nil {
		return fmt.Errorf("open/create adapter failed: %w", err)
	}

	if err := setAdapterIPv4(adapterName, localIP, localMask); err != nil {
		return fmt.Errorf("set adapter IP failed: %w", err)
	}
	if err := runCommand("netsh", "interface", "ipv4", "set", "subinterface", adapterName, "mtu="+tunMTU, "store=active"); err != nil {
		return fmt.Errorf("set adapter MTU failed: %w", err)
	}

	tunnelIfIndex, ifErr := getInterfaceIndexByName(adapterName)
	if ifErr != nil {
		logging.Errorf("could not resolve tunnel interface index for %s: %v", adapterName, ifErr)
	} else {
		logging.Infof("tunnel interface index resolved adapter=%s ifIndex=%s", adapterName, tunnelIfIndex)
	}

	session, err := adapter.StartSession(ringBytes)
	if err != nil {
		// Adapter may have a leaked session from a previous cancelled connect.
		// Invalidate, close, recreate and retry once.
		logging.Infof("start_session_failed_retrying adapter=%s err=%v", adapterName, err)
		invalidateCachedAdapter(adapterName)
		_ = adapter.Close()
		adapter, err = getOrCreateAdapter(adapterName)
		if err != nil {
			return fmt.Errorf("start wintun session failed (adapter recreate): %w", err)
		}
		session, err = adapter.StartSession(ringBytes)
		if err != nil {
			invalidateCachedAdapter(adapterName)
			_ = adapter.Close()
			return fmt.Errorf("start wintun session failed: %w", err)
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
		session.End()
		// Keep adapter alive for reuse; auth/network failures should not churn adapter identity.
		return fmt.Errorf("ssh dial failed: %w", err)
	}

	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], tunModePointToPoint)
	binary.BigEndian.PutUint32(payload[4:8], uint32(tunSlot))

	tunCh, reqs, err := sshConn.OpenChannel("tun@openssh.com", payload)
	if err != nil {
		session.End()
		sshConn.Close()
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
		server.RemoteIP, localIP, serverTunDev,
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

	if err := ensureRoute(lanRouteIP, lanMask, server.RemoteIP, tunnelIfIndex); err != nil {
		logging.Errorf("route_add_failed route=%s mask=%s gw=%s err=%v", lanRouteIP, lanMask, server.RemoteIP, err)
	}

	at := &activeTunnel{
		adapter:     adapter,
		session:     session,
		sshConn:     sshConn,
		tunCh:       tunCh,
		adapterName: adapterName,
		tunSlot:     tunSlot,
		lanRouteIP:  lanRouteIP,
		lanMask:     lanMask,
		lanGateway:  server.RemoteIP,
		fullTunnel:  server.FullTunnel,
		connectedAt: time.Now(),
	}
	at.closed.Store(false)

	// Set up full tunnel routes if enabled
	if at.fullTunnel {
		gw, err := defaultGateway()
		if err != nil {
			logging.Errorf("full_tunnel: cannot detect default gateway: %v", err)
		} else {
			at.originalGW = gw
			logging.Infof("full_tunnel: enabled, original_gw=%s", gw)
			// Keep SSH server reachable via original gateway
			gatewayHost := server.Gateway
			if !strings.Contains(gatewayHost, ":") {
				gatewayHost += ":22"
			}
			sshHostIP := strings.Split(gatewayHost, ":")[0]
			if err := ensureRoute(sshHostIP, "255.255.255.255", gw, ""); err != nil {
				logging.Errorf("full_tunnel: failed to pin ssh host route host=%s gw=%s err=%v", sshHostIP, gw, err)
			}
			// Route all IPv4 via tunnel using two /1 routes
			if err := ensureRoute("0.0.0.0", "128.0.0.0", server.RemoteIP, tunnelIfIndex); err != nil {
				logging.Errorf("full_tunnel: failed to set lower /1 route via %s err=%v", server.RemoteIP, err)
			}
			if err := ensureRoute("128.0.0.0", "128.0.0.0", server.RemoteIP, tunnelIfIndex); err != nil {
				logging.Errorf("full_tunnel: failed to set upper /1 route via %s err=%v", server.RemoteIP, err)
			}
			logging.Infof("full_tunnel: all internet routed via %s (SSH host %s via original gateway)", server.RemoteIP, sshHostIP)
		}
	}

	at.startBridge()

	tm.mu.Lock()
	tm.state = at
	tm.mu.Unlock()

	logging.Infof("tunnel_connect_ok adapter=%s route=%s/%s", adapterName, lanRouteIP, lanMask)
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
	logging.Infof("tunnel_disconnect_ok adapter=%s", at.adapterName)
	return nil
}

func (at *activeTunnel) startBridge() {
	readWait := at.session.ReadWaitEvent()

	go func() {
		for {
			if at.closed.Load() {
				return
			}
			_, _ = windows.WaitForSingleObject(readWait, 500)
			if at.closed.Load() {
				return
			}
			for {
				if at.closed.Load() {
					return
				}
				pkt, err := at.session.ReceivePacket()
				if err != nil {
					if err == windows.ERROR_NO_MORE_ITEMS {
						break
					}
					return
				}

				af := afInet
				if len(pkt) > 0 && pkt[0]>>4 == 6 {
					af = afInet6
				}

				frame := make([]byte, 4+len(pkt))
				binary.BigEndian.PutUint32(frame[0:4], af)
				copy(frame[4:], pkt)
				at.txBytes.Add(uint64(len(pkt)))
				at.session.ReleaseReceivePacket(pkt)

				if _, err := at.tunCh.Write(frame); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		buf := make([]byte, 4+65535)
		for {
			if at.closed.Load() {
				return
			}
			n, err := at.tunCh.Read(buf)
			if err != nil {
				return
			}
			if n <= 4 {
				continue
			}
			ipPkt := buf[4:n]
			at.rxBytes.Add(uint64(len(ipPkt)))
			if at.closed.Load() {
				return
			}
			dst, err := at.session.AllocateSendPacket(len(ipPkt))
			if err != nil {
				return
			}
			copy(dst, ipPkt)
			at.session.SendPacket(dst)
		}
	}()
}

func (at *activeTunnel) close() {
	if at.closed.Swap(true) {
		return
	}

	if at.tunCh != nil {
		_ = at.tunCh.Close()
	}

	// Clean up server-side iptables rules (before closing SSH)
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

	at.session.End()
	_ = runCommand("route", "delete", at.lanRouteIP, "mask", at.lanMask, at.lanGateway)

	// Clean up full-tunnel routes if enabled
	if at.fullTunnel && at.originalGW != "" {
		logging.Infof("full_tunnel: cleaning up routes")
		_ = runCommand("route", "delete", "0.0.0.0", "mask", "128.0.0.0", at.lanGateway)
		_ = runCommand("route", "delete", "128.0.0.0", "mask", "128.0.0.0", at.lanGateway)
	}

	// Keep adapter alive for reuse; do not delete/recreate per connection.
}

func (at *activeTunnel) stats() Stats {
	return Stats{
		Connected:   true,
		ConnectedAt: at.connectedAt,
		RxBytes:     at.rxBytes.Load(),
		TxBytes:     at.txBytes.Load(),
	}
}

func getOrCreateAdapter(adapterName string) (*wintun.Adapter, error) {
	adapterCacheMu.Lock()
	defer adapterCacheMu.Unlock()

	if cached, ok := adapterCache[adapterName]; ok && cached != nil {
		return cached, nil
	}

	if opened, err := wintun.OpenAdapter(adapterName); err == nil {
		adapterCache[adapterName] = opened
		return opened, nil
	}

	guid := deterministicAdapterGUID(adapterName)
	created, err := wintun.CreateAdapter(adapterName, adapterTunnelType, &guid)
	if err != nil {
		return nil, err
	}
	adapterCache[adapterName] = created
	return created, nil
}

func invalidateCachedAdapter(adapterName string) {
	adapterCacheMu.Lock()
	defer adapterCacheMu.Unlock()
	delete(adapterCache, adapterName)
}

func setAdapterIPv4(adapterName, localIP, localMask string) error {
	// Use modern netsh ipv4 syntax; it is less error-prone than legacy interface ip syntax.
	err := runCommand(
		"netsh",
		"interface", "ipv4", "set", "address",
		"name="+adapterName,
		"source=static",
		"address="+localIP,
		"mask="+localMask,
	)
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "object already exists") {
		// If the same address already exists, keep going instead of failing/crashing retries.
		logging.Infof("adapter_ip_already_set adapter=%s ip=%s mask=%s", adapterName, localIP, localMask)
		return nil
	}

	if strings.Contains(msg, "syntax is incorrect") || strings.Contains(msg, "filename, directory name, or volume label syntax is incorrect") {
		// Fallback to legacy syntax on systems where ipv4 set address parser is picky.
		legacyErr := runCommand("netsh", "interface", "ip", "set", "address", adapterName, "static", localIP, localMask)
		if legacyErr == nil {
			return nil
		}
		legacyMsg := strings.ToLower(legacyErr.Error())
		if strings.Contains(legacyMsg, "object already exists") {
			logging.Infof("adapter_ip_already_set_legacy adapter=%s ip=%s mask=%s", adapterName, localIP, localMask)
			return nil
		}
		return legacyErr
	}

	return err
}

func CleanupNamedAdapter(adapterName string) error {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		fmt.Sprintf("$adapter = Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue; if ($adapter) { $adapter | Remove-NetAdapter -Confirm:$false -ErrorAction Stop }", escapePowerShellSingleQuoted(adapterName)),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("remove adapter %s failed: %s", adapterName, text)
	}
	return nil
}

func deterministicAdapterGUID(adapterName string) windows.GUID {
	hash := md5.Sum([]byte("linkthings:wintun:" + strings.ToLower(strings.TrimSpace(adapterName))))
	guid := windows.GUID{
		Data1: binary.LittleEndian.Uint32(hash[0:4]),
		Data2: binary.LittleEndian.Uint16(hash[4:6]),
		Data3: binary.LittleEndian.Uint16(hash[6:8]),
		Data4: [8]byte{hash[8], hash[9], hash[10], hash[11], hash[12], hash[13], hash[14], hash[15]},
	}
	guid.Data3 = (guid.Data3 & 0x0fff) | 0x4000
	guid.Data4[0] = (guid.Data4[0] & 0x3f) | 0x80
	return guid
}

func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w (%s)", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureRoute makes route setup resilient on Windows where route add may fail if entry already exists.
func ensureRoute(destination, mask, gateway, ifIndex string) error {
	addArgs := []string{"add", destination, "mask", mask, gateway}
	changeArgs := []string{"change", destination, "mask", mask, gateway}
	deleteArgs := []string{"delete", destination, "mask", mask, gateway}
	if ifIndex != "" {
		addArgs = append(addArgs, "if", ifIndex)
		changeArgs = append(changeArgs, "if", ifIndex)
		deleteArgs = append(deleteArgs, "if", ifIndex)
	}

	if err := runCommand("route", addArgs...); err == nil {
		return nil
	} else {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "object already exists") && !strings.Contains(msg, "file exists") {
			return err
		}

		if changeErr := runCommand("route", changeArgs...); changeErr == nil {
			return nil
		}

		_ = runCommand("route", deleteArgs...)
		if retryErr := runCommand("route", addArgs...); retryErr == nil {
			return nil
		}

		if ifIndex == "" {
			return fmt.Errorf("route %s/%s via %s exists but could not be updated", destination, mask, gateway)
		}
		return fmt.Errorf("route %s/%s via %s (if %s) exists but could not be updated", destination, mask, gateway, ifIndex)
	}
}

// getInterfaceIndexByName resolves Windows interface index used by `route ... if <index>`.
func getInterfaceIndexByName(adapterName string) (string, error) {
	out, err := exec.Command("netsh", "interface", "ipv4", "show", "interfaces").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("netsh show interfaces failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		name := strings.Join(fields[4:], " ")
		if strings.EqualFold(name, adapterName) {
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("interface %q not found in netsh output", adapterName)
}

func escapePowerShellSingleQuoted(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func ipv4FromCIDR(cidr string) (string, string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return "", "", fmt.Errorf("not an IPv4 CIDR")
	}
	return ipv4.String(), maskToString(ipNet.Mask), nil
}

func networkFromCIDR(cidr string) (string, string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return "", "", fmt.Errorf("not an IPv4 CIDR")
	}
	return ip.String(), maskToString(ipNet.Mask), nil
}

func maskToString(mask net.IPMask) string {
	m := net.IP(mask).To4()
	if m == nil {
		return "255.255.255.0"
	}
	return m.String()
}

func sanitizeAdapterName(serverName string) string {
	name := strings.TrimSpace(serverName)
	if name == "" {
		return "LinkThings-VPN"
	}
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	if len(name) > 64 {
		name = name[:64]
	}
	return "LT-" + name
}

// defaultGateway returns the current IPv4 default gateway by parsing `route print 0.0.0.0`
func defaultGateway() (string, error) {
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" {
			return f[2], nil
		}
	}
	return "", fmt.Errorf("default route not found in routing table")
}
