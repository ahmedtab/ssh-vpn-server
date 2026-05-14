// Package users wraps the manage-users.sh script logic and provides
// a Go API for creating, listing, and removing VPN users.
// The bash script is called via exec so tun-slot allocation and
// authorized_keys tagging remain the single source of truth in phase 1.
package users

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// User represents a VPN user loaded from /etc/vpn/users/<name>.conf.
type User struct {
	Username    string    `json:"username"`
	TunNum      int       `json:"tun_num"`
	ServerIP    string    `json:"server_ip"`
	ClientIP    string    `json:"client_ip"`
	TunStatus   string    `json:"tun_status"` // "up" | "down"
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

const manageScript = "/usr/local/bin/manage-users.sh"

// Manager wraps user operations.
type Manager struct {
	usersDir     string
	authKeysFile string
}

// NewManager creates a Manager.
func NewManager(usersDir, authKeysFile string) *Manager {
	return &Manager{usersDir: usersDir, authKeysFile: authKeysFile}
}

// Add creates a new VPN user with the given SSH public key.
// Returns the created User on success.
func (m *Manager) Add(username, pubkey string) (*User, error) {
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePubkey(pubkey); err != nil {
		return nil, err
	}
	// Delegate to manage-users.sh which handles slot allocation and key tagging.
	out, err := runScript("add", username, pubkey)
	if err != nil {
		return nil, fmt.Errorf("add user: %w — %s", err, out)
	}
	return m.Get(username)
}

// Remove deletes a VPN user by name.
func (m *Manager) Remove(username string) error {
	if err := validateUsername(username); err != nil {
		return err
	}
	_, err := runScript("remove", username)
	return err
}

// List returns all configured VPN users.
func (m *Manager) List() ([]User, error) {
	entries, err := os.ReadDir(m.usersDir)
	if err != nil {
		return nil, fmt.Errorf("read users dir: %w", err)
	}
	var users []User
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".conf")
		u, err := m.Get(name)
		if err != nil {
			continue
		}
		users = append(users, *u)
	}
	return users, nil
}

// Get returns a single user by parsing their .conf file.
func (m *Manager) Get(username string) (*User, error) {
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	path := filepath.Join(m.usersDir, username+".conf")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	defer f.Close()

	u := &User{Username: username}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			// Parse creation timestamp comment if present
			if strings.Contains(line, "Created:") {
				parts := strings.SplitN(line, "Created:", 2)
				if len(parts) == 2 {
					t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
					if err == nil {
						u.CreatedAt = t
					}
				}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "TUN_NUM":
			fmt.Sscan(v, &u.TunNum)
		case "TUN_LOCAL_IP":
			u.ServerIP = v
		case "TUN_REMOTE_IP":
			u.ClientIP = v
		}
	}
	// Probe tun status
	u.TunStatus = tunStatus(fmt.Sprintf("tun%d", u.TunNum))
	return u, nil
}

// Exists reports whether a user config file exists.
func (m *Manager) Exists(username string) bool {
	_, err := os.Stat(filepath.Join(m.usersDir, username+".conf"))
	return err == nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func runScript(args ...string) (string, error) {
	cmd := exec.Command(manageScript, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tunStatus(dev string) string {
	out, err := exec.Command("ip", "link", "show", dev).CombinedOutput()
	if err != nil {
		return "down"
	}
	if strings.Contains(string(out), "UP") {
		return "up"
	}
	return "down"
}

func validateUsername(name string) error {
	if name == "" {
		return fmt.Errorf("username is required")
	}
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			return fmt.Errorf("invalid username %q: only alphanumeric, - and _ are allowed", name)
		}
	}
	if len(name) > 64 {
		return fmt.Errorf("username too long (max 64 chars)")
	}
	return nil
}

func validatePubkey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("public key is required")
	}
	// Must start with a recognized SSH key type prefix to prevent injection.
	validPrefixes := []string{"ssh-ed25519 ", "ssh-rsa ", "ecdsa-sha2-", "sk-ssh-"}
	for _, p := range validPrefixes {
		if strings.HasPrefix(key, p) {
			return nil
		}
	}
	return fmt.Errorf("invalid SSH public key format")
}
