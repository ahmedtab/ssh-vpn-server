//go:build !windows && !linux

package ui

import "fmt"

type unsupportedClipboard struct{}

func currentClipboard() Clipboard { return unsupportedClipboard{} }

func (unsupportedClipboard) Copy(text string) error {
	return fmt.Errorf("clipboard not supported on this platform")
}
