// Package audit writes structured JSON audit events to a log file.
// Every admin action and provisioning request is recorded with actor,
// action, result, and timestamp so operators can review what happened.
package audit

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Event is a single audit record.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Actor     string    `json:"actor"`      // Linux username or "system"
	Action    string    `json:"action"`     // e.g. "user.create", "user.delete", "auth.login"
	Target    string    `json:"target"`     // subject of the action (VPN username, IP, …)
	Result    string    `json:"result"`     // "ok" | "denied" | "error"
	Detail    string    `json:"detail,omitempty"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
}

// Logger writes audit events to a file.
type Logger struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// NewLogger opens (or creates) the audit log file at path.
func NewLogger(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return &Logger{f: f, enc: enc}, nil
}

// Log writes one audit event. It is safe for concurrent use.
func (l *Logger) Log(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(e) // best-effort; disk errors are silent to avoid cascades
}

// Close flushes and closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
