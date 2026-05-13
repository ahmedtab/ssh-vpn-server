//go:build !windows

package tunnel

import (
	"fmt"

	"golang.org/x/crypto/ssh"
	"linkthings.io/client/config"
)

func CleanupOrphanAdapters() error {
	return nil
}

func (tm *TunnelManager) Connect(_ config.ServerConfig, _ ssh.Signer) error {
	return fmt.Errorf("tunnel is only supported on Windows")
}

func (tm *TunnelManager) Disconnect() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.state = nil
	return nil
}
