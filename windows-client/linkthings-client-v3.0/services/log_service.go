package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"linkthings.io/client-v3/logging"
)

// LogEntry is the JS-facing shape of one log line.
type LogEntry struct {
	Time  int64  `json:"time"` // unix seconds
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// NOTE: see the comment in connection_service.go — application.RegisterEvent
// for "log" was tried and reverted for the same eventcreate.ts module-eval-order
// crash. Typed the equivalent by hand in frontend/src/types/events.d.ts instead.

// LogService streams the app's log file to the frontend: History for the
// initial load, then live entries via the "log" event as they're written.
type LogService struct {
	app *application.App
}

func NewLogService(app *application.App) *LogService {
	svc := &LogService{app: app}
	logging.Subscribe(func(e logging.Entry) {
		app.Event.Emit("log", toLogEntry(e))
	})
	return svc
}

// History returns the last n log lines already on disk.
func (s *LogService) History(n int) ([]LogEntry, error) {
	entries, err := logging.Tail(n)
	if err != nil {
		return nil, err
	}
	out := make([]LogEntry, len(entries))
	for i, e := range entries {
		out[i] = toLogEntry(e)
	}
	return out, nil
}

// Path returns the on-disk log file path, shown in the Logs screen header.
func (s *LogService) Path() string {
	return logging.Path()
}

func toLogEntry(e logging.Entry) LogEntry {
	return LogEntry{Time: e.Time.Unix(), Level: e.Level, Msg: e.Msg}
}
