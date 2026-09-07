package ui

// Clipboard is the OS-specific "copy text to the system clipboard" primitive.
// Adding a new OS means implementing this interface (plus a
// currentClipboard() constructor) in one new build-tagged file.
type Clipboard interface {
	Copy(text string) error
}
