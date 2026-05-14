// Package pamauth authenticates Linux users via PAM.
// This allows the web admin to log in using existing OS credentials —
// no separate password database is needed.
package pamauth

import (
	"fmt"

	"github.com/msteinert/pam/v2"
)

// Authenticate verifies username/password against the system PAM stack.
// Uses the "login" PAM service which respects /etc/pam.d/login.
// Returns nil on success, error on failure.
func Authenticate(username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("username and password are required")
	}

	tx, err := pam.StartFunc("login", username, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		case pam.ErrorMsg, pam.TextInfo:
			return "", nil
		default:
			return "", fmt.Errorf("unhandled PAM style: %d", s)
		}
	})
	if err != nil {
		return fmt.Errorf("pam start: %w", err)
	}

	if err := tx.Authenticate(0); err != nil {
		return fmt.Errorf("authentication failed")
	}
	return nil
}
