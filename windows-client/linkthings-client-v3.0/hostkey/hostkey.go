// Package hostkey implements trust-on-first-use (TOFU) SSH host-key pinning,
// replacing the InsecureIgnoreHostKey callback v2 used everywhere. A gateway's
// host-key fingerprint is captured once (Probe), confirmed by the user, and
// persisted on its ServerConfig; every later dial verifies the presented key
// against that pinned fingerprint (VerifyCallback) and fails closed on any
// mismatch rather than silently trusting whoever answers on that address.
package hostkey

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// MismatchError means the host answering at this address presented a
// different key than the one pinned for it — a changed host key (reinstall,
// rotation) or an active MITM. Callers should surface this distinctly from
// an ordinary connection failure rather than retrying silently.
type MismatchError struct {
	Expected string
	Got      string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("host key mismatch: expected %s, got %s — this gateway's key changed, or the connection is being intercepted", e.Expected, e.Got)
}

// Fingerprint returns the SHA256 fingerprint of an SSH public key in the
// same format `ssh-keygen -lf` and OpenSSH's own prompts use.
func Fingerprint(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}

// VerifyCallback returns a HostKeyCallback that accepts only a host key
// whose fingerprint matches expected. expected must be non-empty — callers
// are expected to have already pinned a fingerprint (via Probe + user
// confirmation) before dialing; there is deliberately no "first use auto
// trust" path here, since that would silently reintroduce the same MITM
// exposure this package exists to close.
func VerifyCallback(expected string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := Fingerprint(key)
		if got != expected {
			return &MismatchError{Expected: expected, Got: got}
		}
		return nil
	}
}

var errProbeComplete = errors.New("hostkey: probe complete")

// Probe connects just far enough to capture the gateway's host key
// fingerprint, then aborts the handshake before any authentication is
// attempted — no credentials are ever sent to an unverified host. hostport
// must already include a port (e.g. "1.2.3.4:22").
func Probe(hostport string, timeout time.Duration) (string, error) {
	var fingerprint string

	cfg := &ssh.ClientConfig{
		User: "probe",
		Auth: nil, // never reached: HostKeyCallback always errors first
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint = Fingerprint(key)
			return errProbeComplete
		},
		Timeout: timeout,
	}

	_, err := ssh.Dial("tcp", hostport, cfg)
	// A captured fingerprint means our HostKeyCallback fired and returned
	// errProbeComplete — regardless of exactly how x/crypto/ssh wraps that
	// error on its way back out, the handshake got far enough that this is
	// our own deliberate abort, not a real connection failure.
	if fingerprint != "" {
		return fingerprint, nil
	}
	if err == nil {
		err = errors.New("connection closed before the host key was received")
	}
	return "", fmt.Errorf("probe host key: %w", err)
}
