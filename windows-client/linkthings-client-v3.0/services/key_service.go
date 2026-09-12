package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
	"linkthings.io/client-v3/keymgmt"
)

// KeyInfo is the display payload for the SSH-key screen's "Key details" card.
type KeyInfo struct {
	Line        string `json:"line"` // full "ssh-ed25519 AAAA... " content
	Fingerprint string `json:"fingerprint"`
	Path        string `json:"path"`
	CreatedAt   int64  `json:"createdAt"` // unix seconds; approximated from the file's mtime
}

// KeyService exposes this machine's Ed25519 keypair to the frontend: the
// public key content/fingerprint, regeneration, and importing an externally
// generated keypair to replace it.
type KeyService struct {
	app    *application.App
	keyMgr *keymgmt.KeyManager
}

func NewKeyService(app *application.App, keyMgr *keymgmt.KeyManager) *KeyService {
	return &KeyService{app: app, keyMgr: keyMgr}
}

// GetPublicKey returns the current public key's content, fingerprint, path
// and (approximate) creation time.
func (s *KeyService) GetPublicKey() (*KeyInfo, error) {
	path := s.keyMgr.GetPublicKeyPath()
	content := s.keyMgr.GetPublicKeyContent()
	if strings.HasPrefix(content, "(error reading key") {
		return nil, fmt.Errorf("%s", content)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	info, err := os.Stat(path)
	var createdAt int64
	if err == nil {
		createdAt = info.ModTime().Unix()
	}

	return &KeyInfo{
		Line:        content,
		Fingerprint: ssh.FingerprintSHA256(pubKey),
		Path:        path,
		CreatedAt:   createdAt,
	}, nil
}

// AuthorizedEntry builds the exact restricted authorized_keys line for a
// given tunnel slot, used for display and for the Copy button.
func (s *KeyService) AuthorizedEntry(sshTunnel int) (string, error) {
	content := s.keyMgr.GetPublicKeyContent()
	if strings.HasPrefix(content, "(error reading key") {
		return "", fmt.Errorf("%s", content)
	}
	return fmt.Sprintf(
		`tunnel="%d",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding %s`,
		sshTunnel, content,
	), nil
}

// Regenerate discards the current keypair and generates a fresh one.
func (s *KeyService) Regenerate() error {
	if err := s.keyMgr.DeleteKeys(); err != nil {
		return err
	}
	return s.keyMgr.GenerateKeypair()
}

// Import replaces the current keypair with an externally generated one
// found at privateKeyPath (optionally passphrase-protected). The derived
// public key is written alongside it at the manager's fixed public-key path.
func (s *KeyService) Import(privateKeyPath, passphrase string) error {
	data, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(data)
	}
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	pubBytes := ssh.MarshalAuthorizedKey(signer.PublicKey())

	if err := os.WriteFile(s.keyMgr.GetPrivateKeyPath(), data, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(s.keyMgr.GetPublicKeyPath(), pubBytes, 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	return nil
}

// PickPrivateKeyFile opens a native file picker for choosing a private key
// — used both by Import and by the SSH-auth register flow's key mode.
func (s *KeyService) PickPrivateKeyFile() (string, error) {
	path, err := s.app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title: "Select private key",
	}).PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}
