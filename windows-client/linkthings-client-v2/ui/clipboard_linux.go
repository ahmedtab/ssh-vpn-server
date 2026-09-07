//go:build linux

package ui

import (
	"bytes"
	"fmt"
	"os/exec"
)

type linuxClipboard struct{}

func currentClipboard() Clipboard { return linuxClipboard{} }

// linuxClipboardTools lists candidate clipboard commands in preference
// order: wl-copy for Wayland, then the two common X11 tools.
var linuxClipboardTools = []struct {
	name string
	args []string
}{
	{"wl-copy", nil},
	{"xclip", []string{"-selection", "clipboard"}},
	{"xsel", []string{"--clipboard", "--input"}},
}

func (linuxClipboard) Copy(text string) error {
	for _, tool := range linuxClipboardTools {
		path, err := exec.LookPath(tool.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, tool.args...)
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", tool.name, err)
		}
		return nil
	}
	return fmt.Errorf("no clipboard tool found (tried wl-copy, xclip, xsel) - copy the key manually")
}
