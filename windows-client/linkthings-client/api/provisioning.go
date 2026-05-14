package api

// Package api provides the HTTP client for the vpn-api control plane.
// The provisioning flow generates an HMAC-SHA256 signed request that the
// server validates before automatically adding the user's SSH public key.

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ProvisionRequest matches the server's otptoken.ProvisionRequest structure.
type ProvisionRequest struct {
	Username  string `json:"username"`
	PublicKey string `json:"public_key"`
	Timestamp string `json:"timestamp"` // Unix seconds as string
	Nonce     string `json:"nonce"`     // 16-byte hex
	Signature string `json:"signature"` // HMAC-SHA256 hex
}

// ProvisionResponse is the successful response from the server.
type ProvisionResponse struct {
	Username string `json:"username"`
	TunNum   int    `json:"tun_num"`
	ServerIP string `json:"server_ip"`
	ClientIP string `json:"client_ip"`
}

// Provision sends a signed provisioning request to the control plane server and
// returns the assigned tunnel parameters on success.
//
// secretHex is the shared HMAC secret as a hex string (same as VPN_OTP_SHARED_SECRET).
// publicKey is the SSH public key content (e.g. from keymgmt.GetPublicKeyContent()).
func Provision(provisionURL, username, publicKey, secretHex string) (*ProvisionResponse, error) {
	secret, err := hex.DecodeString(secretHex)
	if err != nil {
		// Fall back to raw bytes if not valid hex
		secret = []byte(secretHex)
	}

	nonce, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := computeSignature(secret, username, publicKey, ts, nonce)

	req := ProvisionRequest{
		Username:  username,
		PublicKey: publicKey,
		Timestamp: ts,
		Nonce:     nonce,
		Signature: sig,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, provisionURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provisioning request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := "provisioning rejected by server"
		if raw, ok := result["error"]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				errMsg = s
			}
		}
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, errMsg)
	}

	var pr ProvisionResponse
	respBody, _ := json.Marshal(result)
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return nil, fmt.Errorf("parse provision response: %w", err)
	}
	return &pr, nil
}

// computeSignature builds the HMAC-SHA256 signature over the canonical message:
//
//	username|pubkey|timestamp|nonce
//
// This must match the server-side otptoken.Validate implementation.
func computeSignature(secret []byte, username, pubkey, ts, nonce string) string {
	msg := username + "|" + pubkey + "|" + ts + "|" + nonce
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
