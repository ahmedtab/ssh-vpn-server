package services

import (
	"fmt"

	"linkthings.io/client-v3/config"
	"linkthings.io/client-v3/keymgmt"
	"linkthings.io/client-v3/logging"
	"linkthings.io/client-v3/sshauth"
)

// AuthorizeRequest is the JS-facing shape for the register flow's second
// modal ("Sign in to register").
type AuthorizeRequest struct {
	ProfileName    string `json:"profileName"`
	Username       string `json:"username"`
	Mode           string `json:"mode"` // "password" | "privatekey"
	Password       string `json:"password,omitempty"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	Passphrase     string `json:"passphrase,omitempty"`
}

// AuthService exposes the SSH-based key-authorization flow that replaces
// the old HTTP control-plane provisioning: it dials the gateway directly
// and appends this machine's public key to the target account's
// authorized_keys. Credentials are never persisted.
type AuthService struct {
	cfg    *config.ConfigManager
	keyMgr *keymgmt.KeyManager
}

func NewAuthService(cfg *config.ConfigManager, keyMgr *keymgmt.KeyManager) *AuthService {
	return &AuthService{cfg: cfg, keyMgr: keyMgr}
}

// AuthorizeViaSSH performs the registration and returns the profile's own
// tunnel parameters on success, matching the design's result line.
func (s *AuthService) AuthorizeViaSSH(req AuthorizeRequest) (*sshauth.AuthorizeResult, error) {
	server := s.cfg.GetServer(req.ProfileName)
	if server == nil {
		return nil, fmt.Errorf("profile %q not found", req.ProfileName)
	}

	result, err := sshauth.AuthorizeKey(*server, s.keyMgr, sshauth.AuthorizeParams{
		Username:       req.Username,
		Mode:           sshauth.AuthMode(req.Mode),
		Password:       req.Password,
		PrivateKeyPath: req.PrivateKeyPath,
		Passphrase:     req.Passphrase,
	})
	if err != nil {
		logging.Errorf("ssh_authorize_failed profile=%s err=%v", req.ProfileName, err)
		return nil, err
	}
	logging.Infof("ssh_authorize_ok profile=%s tun=%d", req.ProfileName, result.TunNum)
	return result, nil
}
