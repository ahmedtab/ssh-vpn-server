//go:build windows

package ui

import (
	"bytes"
	"os/exec"
)

type windowsClipboard struct{}

func currentClipboard() Clipboard { return windowsClipboard{} }

func (windowsClipboard) Copy(text string) error {
	cmd := exec.Command("clip")
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}
