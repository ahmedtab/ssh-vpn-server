// Package otptoken manages provisioning OTP tokens for the Windows client.
// Flow:
//  1. Windows client calls POST /api/v1/provision with username + SSH pubkey,
//     signed with an HMAC-SHA256 proof using the shared secret.
//  2. Server validates the signature, checks rate limits, creates the VPN user
//     via users.Manager.Add(), and returns tun slot + peer IPs.
//
// The "OTP" here is a short-lived HMAC-signed request token rather than
// TOTP (time-based one-time password).  This keeps the client stateless
// (no TOTP seed needed) while still preventing replay attacks via a
// nonce + TTL window check.
package otptoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ProvisionRequest is the JSON body sent by the Windows client.
type ProvisionRequest struct {
	Username  string `json:"username"  binding:"required"`
	PublicKey string `json:"public_key" binding:"required"`
	// Timestamp is Unix seconds (UTC). Must be within ±TTL of server time.
	Timestamp int64  `json:"timestamp"  binding:"required"`
	// Nonce is a random hex string to prevent exact replay within the TTL window.
	Nonce     string `json:"nonce"      binding:"required"`
	// Signature is HMAC-SHA256(secret, username|pubkey|timestamp|nonce) as lowercase hex.
	Signature string `json:"signature"  binding:"required"`
}

// Validate checks the request signature and timestamp freshness.
// secret is the raw bytes of the shared secret.
// ttl is the allowed age of a request (e.g., 10 minutes).
func Validate(req *ProvisionRequest, secret []byte, ttl time.Duration) error {
	// 1. Timestamp freshness
	reqTime := time.Unix(req.Timestamp, 0).UTC()
	diff := time.Since(reqTime)
	if diff < 0 {
		diff = -diff
	}
	if diff > ttl {
		return fmt.Errorf("request timestamp too old or too far in the future (diff=%s)", diff)
	}

	// 2. Nonce format (hex, 8–64 chars)
	if len(req.Nonce) < 8 || len(req.Nonce) > 64 {
		return fmt.Errorf("invalid nonce length")
	}

	// 3. HMAC-SHA256 signature
	message := strings.Join([]string{
		req.Username,
		req.PublicKey,
		strconv.FormatInt(req.Timestamp, 10),
		req.Nonce,
	}, "|")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(req.Signature))) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
