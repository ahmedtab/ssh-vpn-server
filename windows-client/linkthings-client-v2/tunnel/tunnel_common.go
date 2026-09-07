package tunnel

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// runCommand runs an external command and folds its combined output into the
// returned error, so callers get useful diagnostics without extra plumbing.
func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w (%s)", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ipv4FromCIDR parses a host CIDR (e.g. "10.0.0.2/24") into its host address
// and dotted-decimal netmask.
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

// networkFromCIDR parses a network CIDR (e.g. "192.168.1.0/24") into its
// network address and dotted-decimal netmask.
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

// parseDNSServers splits a comma-separated DNS string into individual IP strings.
func parseDNSServers(dns string) []string {
	if dns == "" {
		return nil
	}
	parts := strings.Split(dns, ",")
	servers := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			servers = append(servers, p)
		}
	}
	return servers
}
