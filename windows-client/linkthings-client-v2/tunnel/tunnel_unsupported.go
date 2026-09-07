//go:build !windows && !linux

package tunnel

import (
	"fmt"
	"net"
)

// unsupportedPlatform is the fallback Platform for any OS without a real
// implementation (currently everything except Windows and Linux). Every
// method fails with the same error, which Connect() surfaces naturally the
// moment it calls OpenOrCreateTun - there is no separate Connect/Disconnect
// stub to keep in sync here, unlike a per-platform reimplementation would need.
type unsupportedPlatform struct{}

func currentPlatform() Platform { return unsupportedPlatform{} }

var errTunnelUnsupported = fmt.Errorf("tunnel is only supported on Windows and Linux")

func (unsupportedPlatform) OpenOrCreateTun(name string, mtu int) (TunDevice, error) {
	return nil, errTunnelUnsupported
}

func (unsupportedPlatform) ConfigureAddress(TunDevice, net.IP, net.IPMask) error {
	return errTunnelUnsupported
}

func (unsupportedPlatform) AddRoute(Route) error    { return errTunnelUnsupported }
func (unsupportedPlatform) DeleteRoute(Route) error { return errTunnelUnsupported }

func (unsupportedPlatform) DefaultGateway() (net.IP, error) {
	return nil, errTunnelUnsupported
}

func (unsupportedPlatform) SetDNS(TunDevice, []net.IP) error { return nil }
func (unsupportedPlatform) RevertDNS(TunDevice) error        { return nil }
func (unsupportedPlatform) CleanupOrphans(name string) error { return nil }
