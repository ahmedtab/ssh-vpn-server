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

	// What "elevated" means is platform-specific and deliberately asymmetric:
	// on Windows the whole process (window + tray) runs elevated via UAC for
	// its entire lifetime, same as the TUI this was forked from - Windows has
	// no equivalent of the Linux problem below. On Linux, IsAdmin/
	// RelaunchElevated instead grant just the CAP_NET_ADMIN file capability
	// tunnel/adapter/route setup needs and re-exec unprivileged - the process
	// never becomes root, because a root process here cannot join the
	// invoking user's own D-Bus session bus (confirmed empirically), which
	// breaks the system tray and the SingleInstance guard below. See
	// elevation_linux.go and CLAUDE.md's operational caveat.
	el := currentElevator()
	if !el.IsAdmin() {
		if el.RelaunchElevated() {
			logging.Infof("relaunching with elevation")
			return
		}
		logging.Errorf("administrator privileges required")
		fmt.Println("Error: This application requires administrator privileges to create network adapters.")
		fmt.Println("Please run as administrator (Windows) or grant network permission when prompted (Linux).")
		el.ShowElevationRequiredMessage()
		os.Exit(1)
	}
	logging.Infof("application started as administrator")

	// tunnel.CleanupOrphanAdapters (and every other privileged operation
	// tunnel/tunnel_linux.go performs) raises CAP_NET_ADMIN into its own
	// ambient set only for the brief span of its own ip/resolvectl calls,
	// not process-wide - see tunnel/ambient_linux.go for why holding it any
	// longer than that collides with WebKitGTK's own bwrap sandbox once the
	// window below is created.
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

	// window is assigned further down (once the main window is created) but
	// is referenced by the SingleInstance callback below - same
	// forward-reference idiom as connectionSvc further down, safe because
	// the callback can only fire after this process's own app.Run() starts.
	var window *application.WebviewWindow

	appOptions := application.Options{
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
	}
	// Safe to enable unconditionally on both platforms: this process is
	// always running as its own real user with a real desktop session by the
	// time it reaches here (Windows: same user SID, just elevated via UAC;
	// Linux: never becomes root at all, see the elevation gate comment
	// above) - there's no D-Bus-session-reachability gate needed anymore.
	appOptions.SingleInstance = &application.SingleInstanceOptions{
		UniqueID: "io.linkthings.client-v3",
		OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			logging.Infof("second_instance_blocked args=%v", data.Args)
			if window != nil {
				window.Show()
				window.Focus()
			}
		},
	}
	app := application.New(appOptions)

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
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
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
