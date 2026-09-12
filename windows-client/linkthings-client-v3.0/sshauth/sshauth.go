// Package sshauth implements the direct SSH key-authorization flow: instead
// of calling a separate HTTP control-plane (the v2 "OTP provisioning" flow,
// removed in v3), the client opens a one-off SSH session straight to the
// gateway, authenticating with a username plus either a password or an
// existing private key, and appends this machine's own public key (with the
// same tunnel-restricted authorized_keys entry the app already displays on
// its SSH-key screen) to that account's ~/.ssh/authorized_keys.
//
// Credentials passed in AuthorizeParams are used for a single SSH dial and
// are never persisted to servers.json or anywhere else.
package sshauth

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"linkthings.io/client-v3/config"
	"linkthings.io/client-v3/hostkey"
	"linkthings.io/client-v3/keymgmt"
)

// AuthMode selects how the one-off SSH session authenticates.
type AuthMode string

const (
	AuthModePassword   AuthMode = "password"
	AuthModePrivateKey AuthMode = "privatekey"
)

// AuthorizeParams carries the credentials for a single authorization
// attempt. Exactly one of Password or PrivateKeyPath is used, based on Mode.
type AuthorizeParams struct {
	Username       string
	Mode           AuthMode
	Password       string
	PrivateKeyPath string
	Passphrase     string
}

// AuthorizeResult echoes the profile's own tunnel parameters back to the
// caller, matching the design's result line ("tun_num N · server_ip ·
// client_ip").
type AuthorizeResult struct {
	TunNum   int
	ServerIP string
	ClientIP string
}

// AuthorizeKey dials the server's gateway directly over SSH using the given
// credentials, then appends this machine's restricted public-key line to the
// authenticated account's authorized_keys file if it isn't already present.
func AuthorizeKey(server config.ServerConfig, keyMgr *keymgmt.KeyManager, p AuthorizeParams) (*AuthorizeResult, error) {
	if err := server.Validate(); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	if p.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if server.HostKeyFingerprint == "" {
		return nil, fmt.Errorf("gateway host key not verified for %q — trust its fingerprint first", server.Name)
	}

	authMethod, err := buildAuthMethod(p)
	if err != nil {
		return nil, err
	}

	gatewayHost := server.Gateway
	if !strings.Contains(gatewayHost, ":") {
		gatewayHost += ":22"
	}

	sshCfg := &ssh.ClientConfig{
		User:            p.Username,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: hostkey.VerifyCallback(server.HostKeyFingerprint),
		Timeout:         15 * time.Second,
	}

	conn, err := ssh.Dial("tcp", gatewayHost, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial failed: %w", err)
	}
	defer conn.Close()

	pubKey := keyMgr.GetPublicKeyContent()
	if strings.HasPrefix(pubKey, "(error reading key") {
		return nil, fmt.Errorf("local public key not found — generate/regenerate it first")
	}
	entry := fmt.Sprintf(
		`tunnel="%d",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding %s`,
		server.SSHTunnel, pubKey,
	)

	cmd := fmt.Sprintf(
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && "+
			"grep -qxF '%s' ~/.ssh/authorized_keys || echo '%s' >> ~/.ssh/authorized_keys",
		shellSingleQuote(entry), shellSingleQuote(entry),
	)

	sess, err := conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open ssh session failed: %w", err)
	}
	defer sess.Close()

	if out, err := sess.CombinedOutput(cmd); err != nil {
		return nil, fmt.Errorf("remote authorized_keys update failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return &AuthorizeResult{
		TunNum:   server.SSHTunnel,
		ServerIP: server.RemoteIP,
		ClientIP: stripCIDR(server.LocalIP),
	}, nil
}

func buildAuthMethod(p AuthorizeParams) (ssh.AuthMethod, error) {
	switch p.Mode {
	case AuthModePassword:
		if p.Password == "" {
			return nil, fmt.Errorf("password is required")
		}
		return ssh.Password(p.Password), nil
	case AuthModePrivateKey:
		if p.PrivateKeyPath == "" {
			return nil, fmt.Errorf("private key path is required")
		}
		keyData, err := os.ReadFile(p.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
		var signer ssh.Signer
		if p.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(p.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	default:
		return nil, fmt.Errorf("unknown auth mode %q", p.Mode)
	}
}

// shellSingleQuote escapes a string for safe embedding inside single quotes
// in a POSIX shell command line.
func shellSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// stripCIDR returns just the host address of a CIDR string (e.g.
// "10.10.11.1/30" -> "10.10.11.1"), or the input unchanged if it isn't CIDR.
func stripCIDR(cidr string) string {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return cidr
	}
	return ip.String()
}
