package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
	"linkthings.io/client-v3/config"
	"linkthings.io/client-v3/keymgmt"
	"linkthings.io/client-v3/logging"
	"linkthings.io/client-v3/services"
	"linkthings.io/client-v3/tunnel"
)

// Version is set during build.
var Version = "3.0.0"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := logging.Init(); err != nil {
		log.Fatalf("Failed to initialize logging: %v", err)
	}
	defer logging.Close()

	// Same elevation gate as the TUI: the whole app (window + tray) runs
	// elevated for its entire lifetime, since tunnel/adapter/route setup
	// needs it and there's no privileged-helper split in this version.
	el := currentElevator()
	if !el.IsAdmin() {
		if el.RelaunchElevated() {
			logging.Infof("relaunching with elevation")
			return
		}
		logging.Errorf("administrator privileges required")
		fmt.Println("Error: This application requires administrator privileges.")
		fmt.Println("Please run as administrator.")
		el.ShowElevationRequiredMessage()
		os.Exit(1)
	}
	logging.Infof("application started as administrator")

	if err := tunnel.CleanupOrphanAdapters(); err != nil {
		logging.Errorf("orphan adapter cleanup failed: %v", err)
	} else {
		logging.Infof("orphan adapter cleanup complete")
	}

	configMgr, err := config.NewConfigManager()
	if err != nil {
		log.Fatalf("Failed to initialize config manager: %v", err)
	}
	if err := configMgr.Load(); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	logging.Infof("config loaded from %s", configMgr.ConfigPath())

	keyMgr, err := keymgmt.NewKeyManager()
	if err != nil {
		log.Fatalf("Failed to initialize key manager: %v", err)
	}
	if err := keyMgr.EnsureKeys(); err != nil {
		log.Fatalf("Failed to ensure SSH keys: %v", err)
	}
	logging.Infof("ssh keys ready")

	tunnelMgr := tunnel.NewTunnelManager()

	app := application.New(application.Options{
		Name:        "LinkThings",
		Description: "LinkThings SSH VPN client",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})

	// connectionSvc is assigned further down, after the tray-rebuild closure
	// that calls into it is defined below — the closure captures this
	// variable by reference, so the forward reference is safe as long as
	// the closure isn't invoked before the assignment happens.
	var connectionSvc *services.ConnectionService

	profileSvc := services.NewProfileService(configMgr, keyMgr)
	keySvc := services.NewKeyService(app, keyMgr)
	authSvc := services.NewAuthService(configMgr, keyMgr)
	logSvc := services.NewLogService(app)
	elevationSvc := services.NewElevationService(el.IsAdmin())

	app.RegisterService(application.NewService(profileSvc))
	app.RegisterService(application.NewService(keySvc))
	app.RegisterService(application.NewService(authSvc))
	app.RegisterService(application.NewService(logSvc))
	app.RegisterService(application.NewService(elevationSvc))

	// Narrow, fixed-size panel window per the Nocturne design's app shell.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         "LinkThings",
		Width:         412,
		Height:        660,
		DisableResize: true,
	})

	// Closing the window hides it instead of quitting — the app keeps
	// running in the tray, per the background-run requirement.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetLabel("LinkThings")
	// TODO: swap for the app's real shield-mark icon (see the design
	// handoff's "shield-check" asset) — this is the Wails demo icon set,
	// used here only so the tray has something to render out of the box.
	tray.SetDarkModeIcon(icons.SystrayDark)
	tray.SetIcon(icons.SystrayLight)
	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
		} else {
			window.Show()
			window.Focus()
		}
	})

	rebuildTrayMenu := func(snap services.ConnSnapshot) {
		tray.SetTooltip("LinkThings — " + trayStatusLabel(snap.State))

		menu := app.NewMenu()
		menu.Add("Show").OnClick(func(*application.Context) {
			window.Show()
			window.Focus()
		})
		menu.AddSeparator()

		connectItem := menu.Add("Connect")
		connectItem.OnClick(func(*application.Context) {
			if err := connectionSvc.Connect(snap.Profile); err != nil {
				logging.Errorf("tray_connect_failed err=%v", err)
			}
		})
		disconnectItem := menu.Add("Disconnect")
		disconnectItem.OnClick(func(*application.Context) {
			if err := connectionSvc.Disconnect(); err != nil {
				logging.Errorf("tray_disconnect_failed err=%v", err)
			}
		})
		switch snap.State {
		case services.StateIdle:
			disconnectItem.SetEnabled(false)
		default:
			connectItem.SetEnabled(false)
		}

		menu.AddSeparator()
		menu.Add("Exit").OnClick(func(*application.Context) {
			app.Quit()
		})

		tray.SetMenu(menu)
	}

	connectionSvc = services.NewConnectionService(app, configMgr, keyMgr, tunnelMgr, rebuildTrayMenu)
	app.RegisterService(application.NewService(connectionSvc))
	rebuildTrayMenu(connectionSvc.CurrentState())

	if err := app.Run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}

func trayStatusLabel(state services.ConnState) string {
	switch state {
	case services.StateConnected:
		return "Connected"
	case services.StateConnecting:
		return "Connecting…"
	case services.StateDisconnecting:
		return "Disconnecting…"
	default:
		return "Disconnected"
	}
}
