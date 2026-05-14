// Package logs provides reading and streaming of VPN log files for the admin UI.
package logs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry is a single structured log line (JSON from tun-monitor or vpn-api itself).
type Entry struct {
	// Timestamp covers both "time" (slog/gin JSON) and "ts" field names.
	Timestamp time.Time `json:"ts"`
	TimeAlt   time.Time `json:"time"` // populated from slog/gin output
	Level     string    `json:"level,omitempty"`
	Msg       string    `json:"msg,omitempty"`
	// Raw is set when the line is not valid JSON (e.g. legacy shell log lines).
	Raw string `json:"raw,omitempty"`
}

// Reader reads log files from the VPN log directory.
type Reader struct {
	logsDir string
}

// NewReader creates a Reader.
func NewReader(logsDir string) *Reader {
	return &Reader{logsDir: logsDir}
}

// ReadTail reads the last n lines from the given log file.
// filename must be a base name only (no path traversal).
func (r *Reader) ReadTail(filename string, n int) ([]Entry, error) {
	if err := validateFilename(filename); err != nil {
		return nil, err
	}
	path := filepath.Join(r.logsDir, filename)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("log file not found: %s", filename)
	}
	defer f.Close()

	// Collect all lines, then return the last n.
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return parseLines(lines), nil
}

// StreamTo writes new lines appended to filename into w until ctx is done.
// Used for the Server-Sent Events log endpoint.
func (r *Reader) StreamTo(filename string, w io.Writer, done <-chan struct{}) error {
	if err := validateFilename(filename); err != nil {
		return err
	}
	path := filepath.Join(r.logsDir, filename)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("log file not found: %s", filename)
	}
	defer f.Close()

	// Seek to end so only new lines are emitted.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			for scanner.Scan() {
				entries := parseLines([]string{scanner.Text()})
				for _, e := range entries {
					b, _ := json.Marshal(e)
					fmt.Fprintf(w, "data: %s\n\n", b)
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}
}

// AvailableFiles returns the list of .log files in the log directory.
func (r *Reader) AvailableFiles() ([]string, error) {
	entries, err := os.ReadDir(r.logsDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseLines(lines []string) []Entry {
	entries := make([]Entry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			e = Entry{Raw: line, Timestamp: time.Now().UTC()}
		} else {
			// slog and Gin output "time" not "ts" — normalise
			if e.Timestamp.IsZero() && !e.TimeAlt.IsZero() {
				e.Timestamp = e.TimeAlt
			}
			e.TimeAlt = time.Time{} // don't send duplicate field to browser
		}
		entries = append(entries, e)
	}
	return entries
}

func validateFilename(name string) error {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid log filename")
	}
	if !strings.HasSuffix(name, ".log") {
		return fmt.Errorf("only .log files may be read")
	}
	return nil
}
