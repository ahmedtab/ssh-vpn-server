package keymgmt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	// SSH keys directory relative to user home
	sshKeyDir = ".ssh"

	// Private key filename
	privateKeyFile = "linkthings_key"

	// Public key filename
	publicKeyFile = "linkthings_key.pub"
)

// KeyManager manages SSH key generation and storage
type KeyManager struct {
	sshDir         string
	privateKeyPath string
	publicKeyPath  string
}

// NewKeyManager creates a new key manager
func NewKeyManager() (*KeyManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	sshDir := filepath.Join(homeDir, sshKeyDir)
	km := &KeyManager{
		sshDir:         sshDir,
		privateKeyPath: filepath.Join(sshDir, privateKeyFile),
		publicKeyPath:  filepath.Join(sshDir, publicKeyFile),
	}

	return km, nil
}

// EnsureKeys generates keys if they don't exist
func (km *KeyManager) EnsureKeys() error {
	// Check if keys already exist
	if km.KeysExist() {
		return nil
	}

	// Create .ssh directory if it doesn't exist
	if err := os.MkdirAll(km.sshDir, 0700); err != nil {
		return fmt.Errorf("failed to create .ssh directory: %w", err)
	}

	// Generate new keypair
	return km.GenerateKeypair()
}

// GenerateKeypair generates a new Ed25519 keypair
func (km *KeyManager) GenerateKeypair() error {
	// Generate Ed25519 keypair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate keypair: %w", err)
	}

	// Encode private key to OpenSSH PEM format
	privKeyPem, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	privateKeyBytes := pem.EncodeToMemory(privKeyPem)

	// Write private key to file with restricted permissions
	if err := os.WriteFile(km.privateKeyPath, privateKeyBytes, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Generate SSH public key
	publicKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("failed to generate public key: %w", err)
	}

	// Write public key to file
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)
	if err := os.WriteFile(km.publicKeyPath, publicKeyBytes, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// KeysExist checks if keys already exist
func (km *KeyManager) KeysExist() bool {
	_, privErr := os.Stat(km.privateKeyPath)
	_, pubErr := os.Stat(km.publicKeyPath)
	return privErr == nil && pubErr == nil
}

// GetPrivateKey returns the private key for SSH connection
func (km *KeyManager) GetPrivateKey() (ssh.Signer, error) {
	privateKeyData, err := os.ReadFile(km.privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return signer, nil
}

// GetPublicKeyPath returns the path to the public key file
func (km *KeyManager) GetPublicKeyPath() string {
	return km.publicKeyPath
}

// GetPublicKeyContent reads and returns the public key string, or an error message.
func (km *KeyManager) GetPublicKeyContent() string {
	data, err := os.ReadFile(km.publicKeyPath)
	if err != nil {
		return fmt.Sprintf("(error reading key: %v)", err)
	}
	return strings.TrimSpace(string(data))
}

// GetPrivateKeyPath returns the path to the private key file
func (km *KeyManager) GetPrivateKeyPath() string {
	return km.privateKeyPath
}

// DeleteKeys deletes both private and public keys
func (km *KeyManager) DeleteKeys() error {
	if err := os.Remove(km.privateKeyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete private key: %w", err)
	}

	if err := os.Remove(km.publicKeyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete public key: %w", err)
	}

	return nil
}
