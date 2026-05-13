package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"linkthings.io/client/config"
	"linkthings.io/client/keymgmt"
	"linkthings.io/client/logging"
	"linkthings.io/client/tunnel"
	"linkthings.io/client/ui"
)

// Version is set during build
var Version = "1.0.0"

func main() {
	if err := logging.Init(); err != nil {
		log.Fatalf("Failed to initialize logging: %v", err)
	}
	defer logging.Close()

	// Check for admin privileges on Windows
	if !isAdmin() {
		if relaunchElevated() {
			logging.Infof("relaunching with elevation")
			return
		}
		logging.Errorf("administrator privileges required")
		fmt.Println("Error: This application requires administrator privileges.")
		fmt.Println("Please run as administrator.")
		showElevationRequiredMessage()
		os.Exit(1)
	}
	logging.Infof("application started as administrator")
	if err := tunnel.CleanupOrphanAdapters(); err != nil {
		logging.Errorf("orphan adapter cleanup failed: %v", err)
	} else {
		logging.Infof("orphan adapter cleanup complete")
	}

	// Initialize configuration manager
	configMgr, err := config.NewConfigManager()
	if err != nil {
		logging.Errorf("config manager init failed: %v", err)
		log.Fatalf("Failed to initialize config manager: %v", err)
	}

	// Load or create configuration
	if err := configMgr.Load(); err != nil {
		logging.Errorf("config load failed: %v", err)
		log.Fatalf("Failed to load configuration: %v", err)
	}
	logging.Infof("config loaded from %s", configMgr.ConfigPath())

	// Initialize key manager
	keyMgr, err := keymgmt.NewKeyManager()
	if err != nil {
		logging.Errorf("key manager init failed: %v", err)
		log.Fatalf("Failed to initialize key manager: %v", err)
	}

	// Ensure SSH keys exist (generate if first run)
	if err := keyMgr.EnsureKeys(); err != nil {
		logging.Errorf("ensure keys failed: %v", err)
		log.Fatalf("Failed to ensure SSH keys: %v", err)
	}
	logging.Infof("ssh keys ready")

	// Create TUI model
	tunnelMgr := tunnel.NewTunnelManager()
	mainModel := ui.NewMainModel(configMgr, keyMgr, tunnelMgr)

	// Start TUI application
	if err := runTUI(mainModel); err != nil {
		logging.Errorf("tui run failed: %v", err)
		log.Fatalf("TUI error: %v", err)
	}
}

// runTUI starts the Bubble Tea TUI application
func runTUI(model tea.Model) error {
	p := tea.NewProgram(model)

	_, err := p.Run()
	return err
}
