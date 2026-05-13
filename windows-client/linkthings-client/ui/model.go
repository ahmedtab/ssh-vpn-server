package ui

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"linkthings.io/client/config"
	"linkthings.io/client/keymgmt"
	"linkthings.io/client/logging"
	"linkthings.io/client/tunnel"
)

// Screen represents different UI screens
type Screen int

const (
	ScreenServerSelect Screen = iota
	ScreenConnecting
	ScreenConnected
	ScreenDisconnecting
	ScreenError
	ScreenSetup
)

// MainModel is the root model for the TUI application
type MainModel struct {
	screen           Screen
	configManager    *config.ConfigManager
	keyManager       *keymgmt.KeyManager
	tunnelManager    *tunnel.TunnelManager
	selectedServer   string
	connectionLog    []string
	errorMessage     string
	width            int
	height           int
	serverListIndex  int
	pubKeyContent    string
	clipboardOK      bool
	stats            tunnel.Stats
	exitOnDisconnect bool
}

// NewMainModel creates a new main model
func NewMainModel(configManager *config.ConfigManager, keyManager *keymgmt.KeyManager, tunnelManager *tunnel.TunnelManager) *MainModel {
	return &MainModel{
		screen:          ScreenServerSelect,
		configManager:   configManager,
		keyManager:      keyManager,
		tunnelManager:   tunnelManager,
		connectionLog:   []string{},
		serverListIndex: 0,
	}
}

// Init implements tea.Model
func (m *MainModel) Init() tea.Cmd {
	return cmdStatsTick(m.tunnelManager)
}

// Update implements tea.Model
func (m *MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case StatusMsg:
		m.connectionLog = append(m.connectionLog, msg.Message)
		logging.Infof("ui_status server=%s message=%q", m.selectedServer, msg.Message)
		if len(m.connectionLog) > m.height-10 {
			m.connectionLog = m.connectionLog[1:]
		}

	case clipboardMsg:
		m.clipboardOK = msg.ok
		return m, nil

	case statsMsg:
		m.stats = msg.stats
		if m.screen == ScreenConnected || m.screen == ScreenConnecting || m.screen == ScreenDisconnecting {
			return m, cmdStatsTick(m.tunnelManager)
		}
		return m, nil

	case ConnectMsg:
		if msg.Error != nil {
			m.screen = ScreenError
			m.errorMessage = msg.Error.Error()
			m.connectionLog = append(m.connectionLog, "Connection failed: "+msg.Error.Error())
			logging.Errorf("connect_failed server=%s err=%v", msg.Server, msg.Error)
			return m, nil
		}

		m.screen = ScreenConnected
		m.connectionLog = nil
		m.stats = m.tunnelManager.Stats()
		logging.Infof("connect_success server=%s", msg.Server)
		return m, cmdStatsTick(m.tunnelManager)

	case DisconnectMsg:
		if msg.Error != nil {
			m.screen = ScreenError
			m.errorMessage = msg.Error.Error()
			m.connectionLog = append(m.connectionLog, "Disconnect failed: "+msg.Error.Error())
			logging.Errorf("disconnect_failed err=%v", msg.Error)
			return m, nil
		}

		shouldQuit := m.exitOnDisconnect
		m.exitOnDisconnect = false
		m.stats = tunnel.Stats{}
		logging.Infof("disconnect_success")
		if shouldQuit {
			return m, tea.Quit
		}
		m.screen = ScreenServerSelect
		m.connectionLog = []string{"Disconnected"}
		return m, nil
	}

	return m, nil
}

// handleKeyPress handles keyboard input based on current screen
func (m *MainModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.screen == ScreenConnected {
			m.exitOnDisconnect = true
			m.screen = ScreenDisconnecting
			return m, cmdDisconnect(m.tunnelManager)
		}
		if m.screen == ScreenError {
			m.screen = ScreenServerSelect
			m.errorMessage = ""
		} else if m.screen == ScreenSetup {
			m.screen = ScreenServerSelect
		} else {
			return m, tea.Quit
		}

	case "up":
		if m.screen == ScreenServerSelect {
			if m.serverListIndex > 0 {
				m.serverListIndex--
			}
		}

	case "down":
		if m.screen == ScreenServerSelect {
			servers := m.configManager.GetServers()
			if m.serverListIndex < len(servers)-1 {
				m.serverListIndex++
			}
		}

	case "enter":
		if m.screen == ScreenServerSelect {
			servers := m.configManager.GetServers()
			if m.serverListIndex < len(servers) {
				m.selectedServer = servers[m.serverListIndex].Name
				m.screen = ScreenConnecting
				m.connectionLog = []string{fmt.Sprintf("Connecting to: %s", m.selectedServer)}
				logging.Infof("connect_start server=%s", m.selectedServer)
				return m, cmdConnect(m.selectedServer, m.configManager, m.keyManager, m.tunnelManager)
			}
		} else if m.screen == ScreenConnected {
			m.screen = ScreenDisconnecting
			return m, cmdDisconnect(m.tunnelManager)
		} else if m.screen == ScreenError {
			m.screen = ScreenServerSelect
			m.errorMessage = ""
		}

	case "k":
		if m.screen == ScreenServerSelect {
			m.pubKeyContent = m.keyManager.GetPublicKeyContent()
			m.clipboardOK = false
			m.screen = ScreenSetup
			authorizedEntry := `tunnel="0",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding ` + m.pubKeyContent
			return m, cmdCopyToClipboard(authorizedEntry)
		}

	case "b", "esc":
		if m.screen == ScreenSetup {
			m.screen = ScreenServerSelect
		}

	case "d":
		if m.screen == ScreenConnected {
			m.exitOnDisconnect = false
			m.screen = ScreenDisconnecting
			return m, cmdDisconnect(m.tunnelManager)
		}
	}

	return m, nil
}

// View implements bubbletea.Model
func (m *MainModel) View() string {
	switch m.screen {
	case ScreenServerSelect:
		return m.viewServerSelect()
	case ScreenConnecting:
		return m.viewConnecting()
	case ScreenConnected:
		return m.viewConnected()
	case ScreenDisconnecting:
		return m.viewDisconnecting()
	case ScreenError:
		return m.viewError()
	case ScreenSetup:
		return m.viewSetup()
	default:
		return "Unknown screen"
	}
}

// viewServerSelect renders the server selection screen
func (m *MainModel) viewServerSelect() string {
	servers := m.configManager.GetServers()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		Render("🔗 LinkThings Client - Server Selection")

	var content string
	content += "Select a server to connect:\n\n"

	for i, server := range servers {
		prefix := "  "
		if i == m.serverListIndex {
			prefix = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Render("> ")
		}
		content += prefix + server.Name + "\n"
	}

	content += "\nControls: ↑↓ (select) | Enter (connect) | K (setup/key info) | Q (exit)\n"

	return lipgloss.JoinVertical(lipgloss.Top, title, "", content)
}

// viewConnecting renders the connecting screen
func (m *MainModel) viewConnecting() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("3")).
		Render("⏳ Connecting...")

	var logContent string
	for _, line := range m.connectionLog {
		logContent += line + "\n"
	}

	return lipgloss.JoinVertical(lipgloss.Top, title, "", logContent, "\nPress Ctrl+C to cancel...")
}

// viewConnected renders the connected screen
func (m *MainModel) viewConnected() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("2")).
		Render("✓ Connected to: " + m.selectedServer)

	statsContent := "Tunnel activity:\n"
	statsContent += fmt.Sprintf("  Connected for: %s\n", formatConnectedFor(m.stats.ConnectedAt))
	statsContent += fmt.Sprintf("  Received: %s\n", formatBytes(m.stats.RxBytes))
	statsContent += fmt.Sprintf("  Sent: %s\n", formatBytes(m.stats.TxBytes))

	controls := "Controls: D (disconnect to server list) | Q (disconnect and exit)\n"

	return lipgloss.JoinVertical(lipgloss.Top, title, "", statsContent, "", controls)
}

// viewDisconnecting renders the disconnecting screen
func (m *MainModel) viewDisconnecting() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("3")).
		Render("⏳ Disconnecting...")

	return title
}

// viewError renders the error screen
func (m *MainModel) viewError() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("1")).
		Render("✗ Error")

	content := "Error: " + m.errorMessage + "\n\n"
	if p := logging.Path(); p != "" {
		content += "Log file: " + p + "\n\n"
	}
	content += "Press Enter or Q to return to server selection.\n"

	return lipgloss.JoinVertical(lipgloss.Top, title, "", content)
}

// viewSetup renders the SSH key setup / public key info screen
func (m *MainModel) viewSetup() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		Render("🔑 LinkThings Client - SSH Key Setup")

	clipLine := ""
	if m.clipboardOK {
		clipLine = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ Copied to clipboard!") + "\n"
	} else {
		clipLine = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("(Select the key below and copy manually)") + "\n"
	}

	keyPath := m.keyManager.GetPublicKeyPath()

	var out string
	out += "Your SSH public key must be added to the server's authorized_keys.\n\n"
	out += lipgloss.NewStyle().Bold(true).Render("Public key file:") + "\n"
	out += "  " + keyPath + "\n\n"
	out += lipgloss.NewStyle().Bold(true).Render("Public key content:") + "\n"
	out += lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(m.pubKeyContent) + "\n\n"
	out += clipLine
	authorizedEntry := `tunnel="0",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding ` + m.pubKeyContent

	out += lipgloss.NewStyle().Bold(true).Render("Steps to add to GW / VPN server:") + "\n"
	out += "  1. On the server, open:  ~/.ssh/authorized_keys\n"
	out += "  2. Add the following line:\n"
	out += "     " + lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(authorizedEntry) + "\n"
	out += "  3. tunnel=\"0\" grants tun access; all other capabilities are denied.\n"
	out += "  4. Also ensure sshd_config contains: PermitTunnel point-to-point\n\n"
	out += "Controls: B or ESC (back) | Q (exit)\n"

	return lipgloss.JoinVertical(lipgloss.Top, title, "", out)
}

// Message types for TUI events
type StatusMsg struct {
	Message string
}

type ConnectMsg struct {
	Server string
	Error  error
}

type DisconnectMsg struct {
	Error error
}

type statsMsg struct {
	stats tunnel.Stats
}

// clipboardMsg is sent after attempting to copy to clipboard
type clipboardMsg struct{ ok bool }

// cmdCopyToClipboard pipes the given text to clip.exe (Windows clipboard)
func cmdCopyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("clip")
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err != nil {
			return clipboardMsg{ok: false}
		}
		return clipboardMsg{ok: true}
	}
}

func cmdStatsTick(tm *tunnel.TunnelManager) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return statsMsg{stats: tm.Stats()}
	})
}

// Command functions
func cmdConnect(serverName string, cm *config.ConfigManager, km *keymgmt.KeyManager, tm *tunnel.TunnelManager) tea.Cmd {
	return func() tea.Msg {
		logging.Infof("connect_cmd_enter server=%s", serverName)

		server := cm.GetServer(serverName)
		if server == nil {
			return ConnectMsg{Server: serverName, Error: fmt.Errorf("server config not found")}
		}

		if err := server.Validate(); err != nil {
			return ConnectMsg{Server: serverName, Error: fmt.Errorf("invalid server config: %w", err)}
		}

		signer, err := km.GetPrivateKey()
		if err != nil {
			return ConnectMsg{Server: serverName, Error: fmt.Errorf("failed to load private key: %w", err)}
		}

		if err := tm.Connect(*server, signer); err != nil {
			return ConnectMsg{Server: serverName, Error: err}
		}

		return ConnectMsg{Server: serverName}
	}
}

func cmdDisconnect(tm *tunnel.TunnelManager) tea.Cmd {
	return func() tea.Msg {
		if err := tm.Disconnect(); err != nil {
			return DisconnectMsg{Error: err}
		}
		return DisconnectMsg{}
	}
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func formatConnectedFor(connectedAt time.Time) string {
	if connectedAt.IsZero() {
		return "just now"
	}
	d := time.Since(connectedAt).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}
