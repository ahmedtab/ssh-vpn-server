package tunnel

import (
	"io"
	"net"
)

// TunDevice is a virtual network interface exchanging raw IP packets.
//
// Close must cause a pending Read to return an error - the shared bridge loop
// (bridge.go) relies on a plain blocking Read/Write/Close and depends on this
// contract to unblock its reader goroutine during Disconnect. Close must end
// the packet-exchange session only; it must not destroy the underlying OS
// network interface, which is expected to survive disconnects for reuse.
type TunDevice interface {
	io.ReadWriteCloser
	Name() string
}

// Route describes a single route to add or remove.
//
// A nil Gateway means the destination is onlink, reached directly via
// ViaDevice. A nil ViaDevice means the route is not bound to a specific
// interface and is resolved via Gateway alone (e.g. the SSH-host pin route
// used in full-tunnel mode).
type Route struct {
	Dest      net.IPNet
	Gateway   net.IP
	ViaDevice TunDevice
}

// Platform is every OS-specific primitive the shared Connect/Disconnect
// orchestration in connect.go needs. Adding support for a new OS means
// implementing this interface (plus a currentPlatform() constructor) in one
// new build-tagged file - nothing in connect.go or bridge.go changes.
type Platform interface {
	// OpenOrCreateTun returns the stable tunnel device, creating it if
	// necessary, with the given MTU applied.
	OpenOrCreateTun(name string, mtu int) (TunDevice, error)

	// ConfigureAddress sets the device's IPv4 address/mask and brings it up.
	ConfigureAddress(dev TunDevice, ip net.IP, mask net.IPMask) error

	// AddRoute and DeleteRoute manage a single route. AddRoute must be an
	// idempotent upsert (safe to call again for a route that already exists).
	AddRoute(r Route) error
	DeleteRoute(r Route) error

	// DefaultGateway returns the OS's current default gateway, used to pin
	// the SSH control connection off the tunnel's own route in full-tunnel mode.
	DefaultGateway() (net.IP, error)

	// SetDNS scopes the given DNS servers to dev only. Implementations that
	// can't do this safely should log a warning and return nil rather than
	// failing the whole connect.
	SetDNS(dev TunDevice, servers []net.IP) error
	RevertDNS(dev TunDevice) error

	// CleanupOrphans removes any stale tunnel device named name left behind
	// by a crashed prior process.
	CleanupOrphans(name string) error
}
