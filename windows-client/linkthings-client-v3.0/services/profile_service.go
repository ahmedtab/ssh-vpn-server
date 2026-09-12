package services

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"linkthings.io/client-v3/config"
	"linkthings.io/client-v3/hostkey"
	"linkthings.io/client-v3/keymgmt"
)

// ProfileService exposes ServerConfig CRUD to the frontend. All validation
// is delegated to config.ServerConfig.Validate() — the single source of
// truth per the parent CLAUDE.md.
type ProfileService struct {
	cfg    *config.ConfigManager
	keyMgr *keymgmt.KeyManager
}

func NewProfileService(cfg *config.ConfigManager, keyMgr *keymgmt.KeyManager) *ProfileService {
	return &ProfileService{cfg: cfg, keyMgr: keyMgr}
}

// List returns every saved server profile.
func (s *ProfileService) List() []config.ServerConfig {
	return s.cfg.GetServers()
}

// Save creates a new profile (originalName == "") or updates an existing
// one identified by originalName.
func (s *ProfileService) Save(originalName string, server config.ServerConfig) error {
	if originalName == "" {
		return s.cfg.AddServer(server)
	}
	return s.cfg.UpdateServer(originalName, server)
}

// Delete removes a profile by name. The last remaining profile can't be
// deleted, matching the design's confirm-modal rule.
func (s *ProfileService) Delete(name string) error {
	if len(s.cfg.GetServers()) <= 1 {
		return fmt.Errorf("at least one profile must remain")
	}
	return s.cfg.RemoveServer(name)
}

// Duplicate clones a profile under a new, unique name and persists it.
func (s *ProfileService) Duplicate(name string) (*config.ServerConfig, error) {
	src := s.cfg.GetServer(name)
	if src == nil {
		return nil, fmt.Errorf("profile %q not found", name)
	}

	clone := *src
	base := src.Name + " copy"
	clone.Name = base
	for i := 2; s.cfg.GetServer(clone.Name) != nil; i++ {
		clone.Name = fmt.Sprintf("%s %d", base, i)
	}

	if err := s.cfg.AddServer(clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

// Test opens a throwaway SSH session to the profile's gateway (the same
// auth pattern tunnel/connect.go uses to establish the real tunnel) and
// closes it immediately, without opening a tun channel or touching routes.
func (s *ProfileService) Test(server config.ServerConfig) error {
	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}
	if server.HostKeyFingerprint == "" {
		return fmt.Errorf("gateway host key not verified for %q — trust its fingerprint first", server.Name)
	}

	signer, err := s.keyMgr.GetPrivateKey()
	if err != nil {
		return fmt.Errorf("local SSH key unavailable: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostkey.VerifyCallback(server.HostKeyFingerprint),
		Timeout:         15 * time.Second,
	}

	conn, err := ssh.Dial("tcp", normalizeGateway(server.Gateway), sshCfg)
	if err != nil {
		return fmt.Errorf("ssh dial failed: %w", err)
	}
	return conn.Close()
}

// ProbeHostKey connects just far enough to capture the fingerprint of the
// SSH host key presented at gateway, without authenticating — used by the
// frontend's first-connect "trust this host key?" confirmation step.
func (s *ProfileService) ProbeHostKey(gateway string) (string, error) {
	return hostkey.Probe(normalizeGateway(gateway), 15*time.Second)
}

// TrustHostKey pins fingerprint as the expected SSH host key for the named
// profile, persisting it to servers.json. Called after the user confirms a
// ProbeHostKey result.
func (s *ProfileService) TrustHostKey(name, fingerprint string) error {
	server := s.cfg.GetServer(name)
	if server == nil {
		return fmt.Errorf("profile %q not found", name)
	}
	updated := *server
	updated.HostKeyFingerprint = fingerprint
	return s.cfg.UpdateServer(name, updated)
}

// ForgetHostKey clears a profile's pinned host key — needed when a gateway
// legitimately rotates its host key (reinstall, migration) and would
// otherwise permanently fail with a mismatch error.
func (s *ProfileService) ForgetHostKey(name string) error {
	server := s.cfg.GetServer(name)
	if server == nil {
		return fmt.Errorf("profile %q not found", name)
	}
	updated := *server
	updated.HostKeyFingerprint = ""
	return s.cfg.UpdateServer(name, updated)
}

func normalizeGateway(gateway string) string {
	if !strings.Contains(gateway, ":") {
		return gateway + ":22"
	}
	return gateway
}
