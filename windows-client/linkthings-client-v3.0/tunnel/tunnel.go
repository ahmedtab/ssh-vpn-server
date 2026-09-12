package tunnel

import (
	"sync"
	"time"
)

// Stats is the user-facing traffic summary for an active tunnel.
type Stats struct {
	Connected   bool
	ConnectedAt time.Time
	RxBytes     uint64
	TxBytes     uint64
}

// TunnelManager manages the VPN tunnel connection lifecycle.
type TunnelManager struct {
	mu    sync.Mutex
	state any
}

// NewTunnelManager creates a new tunnel manager.
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{}
}

// IsConnected returns whether a tunnel is currently active.
func (tm *TunnelManager) IsConnected() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.state != nil
}

// Stats returns traffic counters for the current tunnel if available.
func (tm *TunnelManager) Stats() Stats {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	provider, ok := tm.state.(interface{ stats() Stats })
	if !ok || provider == nil {
		return Stats{}
	}
	return provider.stats()
}
