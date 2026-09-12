package logging

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"linkthings.io/client-v3/paths"
)

var (
	mu      sync.Mutex
	logger  *log.Logger
	logFile *os.File
	logPath string

	subMu       sync.Mutex
	subscribers = map[int]func(Entry){}
	nextSubID   int
)

// Entry is one parsed log line, used both for live subscriber fan-out and
// for Tail's history read.
type Entry struct {
	Time  time.Time
	Level string
	Msg   string
}

// Subscribe registers fn to be called with every Entry as it's written
// (in addition to the normal file write). It returns an unsubscribe func.
// Used by the GUI's LogService to stream live log lines to the frontend.
func Subscribe(fn func(Entry)) (unsubscribe func()) {
	subMu.Lock()
	id := nextSubID
	nextSubID++
	subscribers[id] = fn
	subMu.Unlock()

	return func() {
		subMu.Lock()
		delete(subscribers, id)
		subMu.Unlock()
	}
}

var lineRE = regexp.MustCompile(`^(\S+ \S+) \[(\w+)\] (.*)$`)

// Tail reads the last n entries already written to the log file. It's a
// best-effort parse of the plain-text log format written by write(); lines
// that don't match are skipped.
func Tail(n int) ([]Entry, error) {
	mu.Lock()
	path := logPath
	mu.Unlock()
	if path == "" {
		return nil, fmt.Errorf("logging not initialized")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n*4 {
			// bound memory on very large log files; we only need the tail
			lines = lines[len(lines)-n*2:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read log file: %w", err)
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	entries := make([]Entry, 0, len(lines))
	for _, line := range lines {
		m := lineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ts, err := time.Parse("2006/01/02 15:04:05.000000", m[1])
		if err != nil {
			ts = time.Time{}
		}
		entries = append(entries, Entry{Time: ts, Level: m[2], Msg: m[3]})
	}
	return entries, nil
}

// Init sets up file logging under the platform's state directory
// (%APPDATA%/LinkThings/logs on Windows, ~/.cache/linkthings/logs on Linux).
func Init() error {
	mu.Lock()
	defer mu.Unlock()

	if logger != nil {
		return nil
	}

	stateDir, err := paths.StateDir()
	if err != nil {
		return fmt.Errorf("failed to determine state directory: %w", err)
	}

	logsDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	logPath = filepath.Join(logsDir, "client.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logFile = f
	logger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("")
	logger.Printf("session_start version=%s", time.Now().Format(time.RFC3339))
	return nil
}

func Infof(format string, args ...any) {
	write("INFO", format, args...)
}

func Errorf(format string, args ...any) {
	write("ERROR", format, args...)
}

func write(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now()

	mu.Lock()
	if logger != nil {
		logger.Printf("[%s] %s", level, msg)
	}
	mu.Unlock()

	subMu.Lock()
	fns := make([]func(Entry), 0, len(subscribers))
	for _, fn := range subscribers {
		fns = append(fns, fn)
	}
	subMu.Unlock()
	entry := Entry{Time: ts, Level: level, Msg: msg}
	for _, fn := range fns {
		fn(entry)
	}
}

func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logger != nil {
		logger.Printf("session_end")
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("")
	logger = nil
	logFile = nil
}
