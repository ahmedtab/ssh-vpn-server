package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"linkthings.io/client-v2/paths"
)

var (
	mu      sync.Mutex
	logger  *log.Logger
	logFile *os.File
	logPath string
)

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
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		return
	}
	logger.Printf("[%s] %s", level, fmt.Sprintf(format, args...))
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
