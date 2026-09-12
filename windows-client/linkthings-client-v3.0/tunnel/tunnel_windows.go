//go:build windows

package tunnel

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"linkthings.io/client-v3/logging"
)

const (
	ringBytes         = 0x400000
	adapterTunnelType = "LinkThings"
)

var (
	adapterCacheMu sync.Mutex
	adapterCache   = map[string]*wintun.Adapter{}
)

type windowsPlatform struct{}

func currentPlatform() Platform { return windowsPlatform{} }

// wintunDevice adapts a Wintun adapter/session pair to the plain blocking
// io.ReadWriteCloser shape TunDevice requires, so the shared bridge loop in
// bridge.go never has to know about Wintun's poll-based ring-buffer API.
type wintunDevice struct {
	adapter  *wintun.Adapter
	session  wintun.Session
	name     string
	readWait windows.Handle
	closed   atomic.Bool
}

func newWintunDevice(adapter *wintun.Adapter, session wintun.Session, name string) *wintunDevice {
	return &wintunDevice{
		adapter:  adapter,
		session:  session,
		name:     name,
		readWait: session.ReadWaitEvent(),
	}
}

func (d *wintunDevice) Name() string { return d.name }

func (d *wintunDevice) Read(p []byte) (int, error) {
	for {
		if d.closed.Load() {
			return 0, io.EOF
		}
		_, _ = windows.WaitForSingleObject(d.readWait, 500)
		if d.closed.Load() {
			return 0, io.EOF
		}
		pkt, err := d.session.ReceivePacket()
		if err != nil {
			if err == windows.ERROR_NO_MORE_ITEMS {
				continue
			}
			return 0, err
		}
		n := copy(p, pkt)
		d.session.ReleaseReceivePacket(pkt)
		return n, nil
	}
}

func (d *wintunDevice) Write(p []byte) (int, error) {
	dst, err := d.session.AllocateSendPacket(len(p))
	if err != nil {
		return 0, err
	}
	copy(dst, p)
	d.session.SendPacket(dst)
	return len(p), nil
}

func (d *wintunDevice) Close() error {
	d.closed.Store(true)
	d.session.End()
	return nil
}

func (windowsPlatform) OpenOrCreateTun(name string, mtu int) (TunDevice, error) {
	adapter, err := getOrCreateAdapter(name)
	if err != nil {
		return nil, fmt.Errorf("open/create adapter failed: %w", err)
	}

	session, err := adapter.StartSession(ringBytes)
	if err != nil {
		// Adapter may have a leaked session from a previous cancelled connect.
		// Invalidate, close, recreate and retry once.
		logging.Infof("start_session_failed_retrying adapter=%s err=%v", name, err)
		invalidateCachedAdapter(name)
		_ = adapter.Close()
		adapter, err = getOrCreateAdapter(name)
		if err != nil {
			return nil, fmt.Errorf("start wintun session failed (adapter recreate): %w", err)
		}
		session, err = adapter.StartSession(ringBytes)
		if err != nil {
			invalidateCachedAdapter(name)
			_ = adapter.Close()
			return nil, fmt.Errorf("start wintun session failed: %w", err)
		}
	}

	if err := runCommand("netsh", "interface", "ipv4", "set", "subinterface", name, fmt.Sprintf("mtu=%d", mtu), "store=active"); err != nil {
		session.End()
		return nil, fmt.Errorf("set adapter MTU failed: %w", err)
	}

	return newWintunDevice(adapter, session, name), nil
}

func (windowsPlatform) ConfigureAddress(dev TunDevice, ip net.IP, mask net.IPMask) error {
	return setAdapterIPv4(dev.Name(), ip.String(), maskToString(mask))
}

func (windowsPlatform) AddRoute(r Route) error {
	destStr := r.Dest.IP.String()
	maskStr := maskToString(r.Dest.Mask)
	gwStr := ""
	if r.Gateway != nil {
		gwStr = r.Gateway.String()
	}

	ifIndex := ""
	if r.ViaDevice != nil {
		idx, err := getInterfaceIndexByName(r.ViaDevice.Name())
		if err != nil {
			logging.Errorf("could not resolve tunnel interface index for %s: %v", r.ViaDevice.Name(), err)
		} else {
			ifIndex = idx
		}
	}

	return ensureRoute(destStr, maskStr, gwStr, ifIndex)
}

func (windowsPlatform) DeleteRoute(r Route) error {
	destStr := r.Dest.IP.String()
	maskStr := maskToString(r.Dest.Mask)
	gwStr := ""
	if r.Gateway != nil {
		gwStr = r.Gateway.String()
	}
	return runCommand("route", "delete", destStr, "mask", maskStr, gwStr)
}

func (windowsPlatform) DefaultGateway() (net.IP, error) {
	s, err := defaultGateway()
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid default gateway %q", s)
	}
	return ip, nil
}

func (windowsPlatform) SetDNS(dev TunDevice, servers []net.IP) error {
	strs := make([]string, len(servers))
	for i, ip := range servers {
		strs[i] = ip.String()
	}
	return setAdapterDNS(dev.Name(), strs)
}

// RevertDNS is a no-op on Windows: DNS is set statically on the persistent
// adapter and is left as-is on disconnect (overwritten by a future connect's
// SetDNS if needed), matching the original implementation's behavior.
func (windowsPlatform) RevertDNS(dev TunDevice) error { return nil }

// CleanupOrphans removes stale LT-* Wintun adapters left behind by crashes.
func (windowsPlatform) CleanupOrphans(name string) error {
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

// setAdapterDNS applies DNS servers to the named network adapter only (not OS-wide).
func setAdapterDNS(adapterName string, servers []string) error {
	if len(servers) == 0 {
		return nil
	}
	// Set the primary DNS server with static assignment
	if err := runCommand("netsh", "interface", "ipv4", "set", "dnsservers",
		"name="+adapterName, "static", servers[0], "primary"); err != nil {
		return fmt.Errorf("set primary DNS failed: %w", err)
	}
	// Add additional DNS servers
	for i := 1; i < len(servers); i++ {
		if err := runCommand("netsh", "interface", "ipv4", "add", "dnsservers",
			"name="+adapterName, servers[i], fmt.Sprintf("index=%d", i+1)); err != nil {
			logging.Errorf("add_dns_server_failed adapter=%s index=%d err=%v", adapterName, i+1, err)
		}
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
