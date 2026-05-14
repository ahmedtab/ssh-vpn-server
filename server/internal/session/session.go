// Package session provides a simple, secure cookie-based session store.
// Sessions are HMAC-signed so the server can verify they haven't been tampered with.
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const cookieName = "vpn_session"

// Data holds the fields stored inside a session.
type Data struct {
	Username  string    `json:"u"`
	CreatedAt time.Time `json:"c"`
	ExpiresAt time.Time `json:"e"`
	CSRF      string    `json:"csrf"`
}

// Store manages session creation and validation.
type Store struct {
	secret []byte
	maxAge time.Duration
	mu     sync.RWMutex
	// Revocation set: sessionID -> true.  Prevents reuse after logout.
	revoked map[string]time.Time
}

// NewStore creates a store with the given HMAC secret and max session age.
func NewStore(secret string, maxAge time.Duration) *Store {
	s := &Store{
		secret:  []byte(secret),
		maxAge:  maxAge,
		revoked: make(map[string]time.Time),
	}
	go s.gc()
	return s
}

// Create mints a new session for username, sets the cookie on w, and returns
// the CSRF token that must be embedded in every state-changing form/request.
func (s *Store) Create(w http.ResponseWriter, username string, secure bool) (csrfToken string, err error) {
	csrf := randomHex(16)
	now := time.Now().UTC()
	d := Data{
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(s.maxAge),
		CSRF:      csrf,
	}
	payload, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.sign(enc)
	cookie := enc + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    cookie,
		Path:     "/",
		MaxAge:   int(s.maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	return csrf, nil
}

// Get validates the session cookie on r and returns the session data.
// Returns an error if the cookie is missing, tampered, expired, or revoked.
func (s *Store) Get(r *http.Request) (*Data, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, fmt.Errorf("no session cookie")
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed session")
	}
	enc, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(s.sign(enc)), []byte(sig)) {
		return nil, fmt.Errorf("invalid session signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, fmt.Errorf("malformed session payload")
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("malformed session data")
	}
	if time.Now().After(d.ExpiresAt) {
		return nil, fmt.Errorf("session expired")
	}
	// Check revocation
	s.mu.RLock()
	_, revoked := s.revoked[enc]
	s.mu.RUnlock()
	if revoked {
		return nil, fmt.Errorf("session revoked")
	}
	return &d, nil
}

// Revoke invalidates the current session cookie (used on logout).
func (s *Store) Revoke(r *http.Request, w http.ResponseWriter) {
	c, err := r.Cookie(cookieName)
	if err == nil {
		parts := strings.SplitN(c.Value, ".", 2)
		if len(parts) == 2 {
			s.mu.Lock()
			s.revoked[parts[0]] = time.Now().Add(s.maxAge)
			s.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ValidateCSRF checks that the X-CSRF-Token header (or _csrf form field) matches the session.
func (s *Store) ValidateCSRF(r *http.Request, d *Data) bool {
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		token = r.FormValue("_csrf")
	}
	return hmac.Equal([]byte(token), []byte(d.CSRF))
}

func (s *Store) sign(data string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// gc periodically removes expired entries from the revocation set.
func (s *Store) gc() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for k, exp := range s.revoked {
			if now.After(exp) {
				delete(s.revoked, k)
			}
		}
		s.mu.Unlock()
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
