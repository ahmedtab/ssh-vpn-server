//go:build linux

package tunnel

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"linkthings.io/client-v2/logging"
)

// IFF_TUN / IFF_NO_PI are not exposed by golang.org/x/sys/unix (only the
// generic ifreq plumbing and TUNSETIFF are); these are the standard, stable
// values from linux/if_tun.h.
const (
	iffTun  = 0x0001
	iffNoPI = 0x1000
)

type linuxPlatform struct{}

func currentPlatform() Platform { return linuxPlatform{} }

// tunFile adapts a raw /dev/net/tun file descriptor to TunDevice. A plain
// *os.File already satisfies io.ReadWriteCloser with the right blocking
// Read/unblock-on-Close semantics (verified against a real TUN fd), so this
// wrapper only adds the device name.
type tunFile struct {
	*os.File
	name string
}

func (t *tunFile) Name() string { return t.name }

func (linuxPlatform) OpenOrCreateTun(name string, mtu int) (TunDevice, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun failed: %w", err)
	}

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("build ifreq for %q failed: %w", name, err)
	}
	ifr.SetUint16(iffTun | iffNoPI)

	// Deliberately do not set TUNSETPERSIST: the interface is torn down
	// automatically when this fd closes, including on a crash, so Linux has
	// no equivalent of Windows's "orphan adapter from a crashed process"
	// problem - CleanupOrphans below is only a defensive fallback.
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("TUNSETIFF failed: %w", err)
	}

	// The fd must be non-blocking before os.NewFile wraps it, or Go's
	// runtime poller won't integrate with it - and without that
	// integration, Close() from another goroutine does not unblock a
	// pending Read() (verified empirically: a blocking-mode TUN fd's Read
	// stays blocked in the kernel across a concurrent Close of the fd
	// number). This is exactly the contract TunDevice.Close documents.
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("set nonblocking failed: %w", err)
	}

	dev := &tunFile{File: os.NewFile(uintptr(fd), name), name: name}

	if err := runCommand("ip", "link", "set", "dev", name, "mtu", strconv.Itoa(mtu)); err != nil {
		_ = dev.Close()
		return nil, fmt.Errorf("set mtu failed: %w", err)
	}
	if err := runCommand("ip", "link", "set", "dev", name, "up"); err != nil {
		_ = dev.Close()
		return nil, fmt.Errorf("bring interface up failed: %w", err)
	}

	return dev, nil
}

func (linuxPlatform) ConfigureAddress(dev TunDevice, ip net.IP, mask net.IPMask) error {
	prefixLen, _ := mask.Size()
	err := runCommand("ip", "addr", "add", fmt.Sprintf("%s/%d", ip.String(), prefixLen), "dev", dev.Name())
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "file exists") {
		logging.Infof("tun_address_already_set dev=%s ip=%s/%d", dev.Name(), ip.String(), prefixLen)
		return nil
	}
	return err
}

func (linuxPlatform) AddRoute(r Route) error {
	args := routeArgs("replace", r)
	return runCommand("ip", args...)
}

func (linuxPlatform) DeleteRoute(r Route) error {
	args := routeArgs("del", r)
	err := runCommand("ip", args...)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such process") || strings.Contains(msg, "cannot find device") {
		// Already gone (e.g. the tunnel device was torn down first) - not an error.
		return nil
	}
	return err
}

// routeArgs builds `ip route <verb> <dest>/<len> [via <gw>] [dev <name>]`.
func routeArgs(verb string, r Route) []string {
	prefixLen, _ := r.Dest.Mask.Size()
	args := []string{"route", verb, fmt.Sprintf("%s/%d", r.Dest.IP.String(), prefixLen)}
	if r.Gateway != nil {
		args = append(args, "via", r.Gateway.String())
	}
	if r.ViaDevice != nil {
		args = append(args, "dev", r.ViaDevice.Name())
	}
	return args
}

func (linuxPlatform) DefaultGateway() (net.IP, error) {
	out, err := exec.Command("ip", "-j", "route", "show", "default").Output()
	if err != nil {
		return nil, fmt.Errorf("ip route show default failed: %w", err)
	}

	var routes []struct {
		Gateway string `json:"gateway"`
	}
	if err := json.Unmarshal(out, &routes); err != nil {
		return nil, fmt.Errorf("parse ip route json failed: %w", err)
	}
	for _, r := range routes {
		if r.Gateway == "" {
			continue
		}
		ip := net.ParseIP(r.Gateway)
		if ip == nil {
			continue
		}
		return ip, nil
	}
	return nil, fmt.Errorf("default route not found")
}

// SetDNS scopes DNS servers to dev via systemd-resolved. If resolvectl isn't
// available, this logs a warning and returns nil rather than failing the
// whole connect or falling back to editing /etc/resolv.conf directly.
func (linuxPlatform) SetDNS(dev TunDevice, servers []net.IP) error {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		logging.Errorf("dns_unsupported: resolvectl not found on PATH, skipping DNS configuration")
		return nil
	}

	args := append([]string{"dns", dev.Name()}, ipStrings(servers)...)
	if err := runCommand("resolvectl", args...); err != nil {
		return fmt.Errorf("resolvectl dns failed: %w", err)
	}
	if err := runCommand("resolvectl", "domain", dev.Name(), "~."); err != nil {
		return fmt.Errorf("resolvectl domain failed: %w", err)
	}
	return nil
}

func (linuxPlatform) RevertDNS(dev TunDevice) error {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return nil
	}
	return runCommand("resolvectl", "revert", dev.Name())
}

// CleanupOrphans is a lightweight defensive check: since OpenOrCreateTun
// never sets TUNSETPERSIST, a stale device from a crashed process is
// structurally rare on Linux (the kernel tears it down when the fd closes),
// unlike Windows's persistent Wintun adapters.
func (linuxPlatform) CleanupOrphans(name string) error {
	if err := exec.Command("ip", "link", "show", name).Run(); err != nil {
		// Not present - nothing to clean up.
		return nil
	}
	if err := runCommand("ip", "link", "delete", name); err != nil {
		return fmt.Errorf("cleanup orphan tun device %s failed: %w", name, err)
	}
	return nil
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}
