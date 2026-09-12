package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"linkthings.io/client-v3/config"
	"linkthings.io/client-v3/keymgmt"
	"linkthings.io/client-v3/logging"
	"linkthings.io/client-v3/tunnel"
)

// ConnState mirrors the design doc's `conn` state enum.
type ConnState string

const (
	StateIdle          ConnState = "idle"
	StateConnecting    ConnState = "connecting"
	StateConnected     ConnState = "connected"
	StateDisconnecting ConnState = "disconnecting"
)

// ConnSnapshot is emitted on every state transition and returned by
// CurrentState() for the frontend's initial load.
type ConnSnapshot struct {
	State       ConnState `json:"state"`
	Profile     string    `json:"profile"`
	ConnectedAt int64     `json:"connectedAt,omitempty"` // unix seconds, 0 if not connected
	Error       string    `json:"error,omitempty"`
}

// StatsSnapshot is emitted every second while a tunnel is up.
type StatsSnapshot struct {
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
	Since   int64  `json:"since"` // unix seconds
}

// NOTE: application.RegisterEvent[T]("conn-state"/"stats") was tried here to
// get generator-typed event payloads, but it makes `wails3 generate bindings
// -ts` emit bindings/.../internal/eventcreate.ts, which eagerly calls
// ConnSnapshot.createFrom/StatsSnapshot.createFrom at module-eval time. In
// the bundled (Rollup) output that file's chunk lands before services/models.ts
// due to a real import cycle through @wailsio/runtime's event system, so the
// classes are still undefined when it runs -> "Cannot read properties of
// undefined (reading 'createFrom')" and the whole app fails to mount. See
// frontend/src/wails-events.d.ts for the hand-written equivalent instead.

// ConnectionService owns the single active-tunnel state machine and is the
// binding surface for the Connect screen and the tray's Connect/Disconnect
// items. Long-running work (the actual SSH dial/teardown) runs off the
// calling goroutine; callers observe progress via the "conn-state" and
// "stats" events rather than a blocking return.
type ConnectionService struct {
	app       *application.App
	cfg       *config.ConfigManager
	keyMgr    *keymgmt.KeyManager
	tunnelMgr *tunnel.TunnelManager

	mu          sync.Mutex
	state       ConnState
	profile     string
	connectedAt time.Time
	stopStats   chan struct{}

	onStateChange func(ConnSnapshot)
}

// onStateChange, if non-nil, is invoked on every transition in addition to
// the "conn-state" event — used by main.go to keep the tray menu/tooltip in
// sync. It's a constructor parameter rather than an exported method so the
// bindings generator never sees a func-typed method on this service (Wails
// binds every exported method, and JS can't receive a Go function value).
func NewConnectionService(app *application.App, cfg *config.ConfigManager, keyMgr *keymgmt.KeyManager, tunnelMgr *tunnel.TunnelManager, onStateChange func(ConnSnapshot)) *ConnectionService {
	return &ConnectionService{
		app:           app,
		cfg:           cfg,
		keyMgr:        keyMgr,
		tunnelMgr:     tunnelMgr,
		state:         StateIdle,
		onStateChange: onStateChange,
	}
}

// CurrentState returns the state snapshot for the frontend's initial load.
func (s *ConnectionService) CurrentState() ConnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked("")
}

func (s *ConnectionService) snapshotLocked(errMsg string) ConnSnapshot {
	snap := ConnSnapshot{State: s.state, Profile: s.profile, Error: errMsg}
	if !s.connectedAt.IsZero() {
		snap.ConnectedAt = s.connectedAt.Unix()
	}
	return snap
}

func (s *ConnectionService) emit(errMsg string) {
	s.mu.Lock()
	snap := s.snapshotLocked(errMsg)
	cb := s.onStateChange
	s.mu.Unlock()

	if s.app != nil {
		s.app.Event.Emit("conn-state", snap)
	}
	if cb != nil {
		cb(snap)
	}
}

// Connect transitions idle -> connecting immediately, then dials the tunnel
// in the background; the eventual connected/idle(error) transition arrives
// via the "conn-state" event.
func (s *ConnectionService) Connect(profileName string) error {
	server := s.cfg.GetServer(profileName)
	if server == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}

	s.mu.Lock()
	if s.state != StateIdle {
		s.mu.Unlock()
		return fmt.Errorf("a tunnel is already %s", s.state)
	}
	s.state = StateConnecting
	s.profile = profileName
	s.mu.Unlock()
	s.emit("")

	go func() {
		signer, err := s.keyMgr.GetPrivateKey()
		if err == nil {
			err = s.tunnelMgr.Connect(*server, signer)
		}

		s.mu.Lock()
		if err != nil {
			logging.Errorf("connect_failed profile=%s err=%v", profileName, err)
			s.state = StateIdle
			s.mu.Unlock()
			s.emit(err.Error())
			return
		}
		s.state = StateConnected
		s.connectedAt = time.Now()
		s.stopStats = make(chan struct{})
		stop := s.stopStats
		s.mu.Unlock()
		logging.Infof("connect_ok profile=%s", profileName)
		s.emit("")
		s.runStatsTicker(stop)
	}()

	return nil
}

// Disconnect transitions connected -> disconnecting immediately, then tears
// the tunnel down in the background.
func (s *ConnectionService) Disconnect() error {
	s.mu.Lock()
	if s.state != StateConnected {
		s.mu.Unlock()
		return fmt.Errorf("no active tunnel to disconnect")
	}
	s.state = StateDisconnecting
	if s.stopStats != nil {
		close(s.stopStats)
		s.stopStats = nil
	}
	s.mu.Unlock()
	s.emit("")

	go func() {
		if err := s.tunnelMgr.Disconnect(); err != nil {
			logging.Errorf("disconnect_failed err=%v", err)
		}

		s.mu.Lock()
		s.state = StateIdle
		s.connectedAt = time.Time{}
		s.mu.Unlock()
		s.emit("")
	}()

	return nil
}

func (s *ConnectionService) runStatsTicker(stop chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			stats := s.tunnelMgr.Stats()
			if !stats.Connected {
				return
			}
			s.app.Event.Emit("stats", StatsSnapshot{
				RxBytes: stats.RxBytes,
				TxBytes: stats.TxBytes,
				Since:   stats.ConnectedAt.Unix(),
			})
		}
	}
}
