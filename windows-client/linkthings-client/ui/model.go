package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"linkthings.io/client/api"
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
	ScreenServerForm
	ScreenDeleteConfirm
	ScreenProvisioning
)

type ServerFormMode int

const (
	ServerFormAdd ServerFormMode = iota
	ServerFormEdit
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
	connectCanceled  bool
	serverFormMode   ServerFormMode
	serverForm       config.ServerConfig
	serverFormOrigin string
	serverFormIndex  int
	serverFormInput  string
	deleteServerName string
	uiNotice         string
	// provisioning
	provisionResult string
	provisionError  string
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
		if m.connectCanceled {
			m.connectCanceled = false
			if msg.Error == nil {
				logging.Infof("connect_canceled_late_success server=%s; disconnecting", msg.Server)
				return m, cmdDisconnect(m.tunnelManager)
			}
			logging.Infof("connect_canceled_result_ignored server=%s err=%v", msg.Server, msg.Error)
			return m, nil
		}

		if msg.Error != nil {
			m.screen = ScreenError
			m.errorMessage = userFacingError(msg.Error)
			m.connectionLog = append(m.connectionLog, "Connection failed: "+m.errorMessage)
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
			m.errorMessage = userFacingError(msg.Error)
			m.connectionLog = append(m.connectionLog, "Disconnect failed: "+m.errorMessage)
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

	case ProvisioningMsg:
		if msg.Error != nil {
			m.provisionError = msg.Error.Error()
			m.provisionResult = ""
		} else {
			m.provisionError = ""
			m.provisionResult = fmt.Sprintf(
				"✓ Provisioned!\n  Tunnel #%d\n  Server: %s\n  Client: %s",
				msg.TunNum, msg.ServerIP, msg.ClientIP,
			)
		}
		return m, nil
	}

	return m, nil
}

// handleKeyPress handles keyboard input based on current screen
func (m *MainModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == ScreenServerForm {
		return m.handleServerFormKey(msg)
	}
	if m.screen == ScreenDeleteConfirm {
		return m.handleDeleteConfirmKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.screen == ScreenConnecting {
			m.connectCanceled = true
			m.screen = ScreenServerSelect
			m.uiNotice = "Connection cancelled"
			logging.Infof("connect_cancel_requested server=%s", m.selectedServer)
			return m, nil
		}
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

	case "q":
		if m.screen == ScreenConnecting {
			m.connectCanceled = true
			m.screen = ScreenServerSelect
			m.uiNotice = "Connection cancelled"
			logging.Infof("connect_cancel_requested server=%s", m.selectedServer)
			return m, nil
		}
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
				m.connectCanceled = false
				m.selectedServer = servers[m.serverListIndex].Name
				m.screen = ScreenConnecting
				m.connectionLog = []string{fmt.Sprintf("Connecting to: %s", m.selectedServer)}
				m.uiNotice = ""
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

	case "p":
		if m.screen == ScreenServerSelect {
			servers := m.configManager.GetServers()
			if m.serverListIndex < len(servers) {
				s := servers[m.serverListIndex]
				if s.ProvisionURL == "" || s.OTPSharedSecret == "" {
					m.uiNotice = "Provisioning not configured for this server (set ProvisionURL + OTPSharedSecret)"
					return m, nil
				}
				m.selectedServer = s.Name
				m.provisionResult = ""
				m.provisionError = ""
				m.screen = ScreenProvisioning
				return m, cmdProvision(s, m.keyManager)
			}
		}

	case "k":
		if m.screen == ScreenServerSelect {
			m.pubKeyContent = m.keyManager.GetPublicKeyContent()
			m.clipboardOK = false
			m.screen = ScreenSetup
			authorizedEntry := `tunnel="0",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding ` + m.pubKeyContent
			return m, cmdCopyToClipboard(authorizedEntry)
		}

	case "a":
		if m.screen == ScreenServerSelect {
			m.startServerForm(ServerFormAdd, "", config.ServerConfig{
				Name:       "New Server",
				Gateway:    "host:2255",
				LocalIP:    "10.10.11.1/30",
				RemoteIP:   "10.10.11.2",
				LANSubnet:  "192.168.4.0/24",
				SSHTunnel:  0,
				MTU:        "1340",
				FullTunnel: true,
				DNS:        "1.1.1.1,8.8.8.8",
			})
		}

	case "e":
		if m.screen == ScreenServerSelect {
			servers := m.configManager.GetServers()
			if len(servers) > 0 && m.serverListIndex < len(servers) {
				s := servers[m.serverListIndex]
				m.startServerForm(ServerFormEdit, s.Name, s)
			}
		}

	case "x":
		if m.screen == ScreenServerSelect {
			servers := m.configManager.GetServers()
			if len(servers) <= 1 {
				m.uiNotice = "At least one server is required"
				return m, nil
			}
			if len(servers) > 0 && m.serverListIndex < len(servers) {
				m.deleteServerName = servers[m.serverListIndex].Name
				m.screen = ScreenDeleteConfirm
			}
		}

	case "b", "esc":
		if m.screen == ScreenSetup {
			m.screen = ScreenServerSelect
		} else if m.screen == ScreenProvisioning {
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
	case ScreenServerForm:
		return m.viewServerForm()
	case ScreenDeleteConfirm:
		return m.viewDeleteConfirm()
	case ScreenProvisioning:
		return m.viewProvisioning()
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

	if m.uiNotice != "" {
		content += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(m.uiNotice) + "\n"
	}

	content += "\nControls: ↑↓ (select) | Enter (connect) | A (add) | E (edit) | X (delete) | K (setup/key info) | Q (exit)\n"

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

func (m *MainModel) viewServerForm() string {
	titleText := "Add Server"
	if m.serverFormMode == ServerFormEdit {
		titleText = "Edit Server"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("🛠 " + titleText)

	labels := []string{"Name", "Gateway", "LocalIP", "RemoteIP", "LANSubnet", "SSHTunnel", "MTU", "FullTunnel", "DNS"}
	values := []string{
		m.serverForm.Name,
		m.serverForm.Gateway,
		m.serverForm.LocalIP,
		m.serverForm.RemoteIP,
		m.serverForm.LANSubnet,
		strconv.Itoa(m.serverForm.SSHTunnel),
		m.serverForm.MTU,
		strconv.FormatBool(m.serverForm.FullTunnel),
		m.serverForm.DNS,
	}

	var body strings.Builder
	body.WriteString("Edit fields then press Ctrl+S to save.\n\n")
	for i := range labels {
		prefix := "  "
		displayValue := values[i]

		// Special handling for FullTunnel toggle
		if i == 7 {
			if m.serverForm.FullTunnel {
				displayValue = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("[X] ON")
			} else {
				displayValue = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[ ] OFF")
			}
			if i == m.serverFormIndex {
				displayValue += lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(" (Space to toggle)")
			}
		}

		if i == m.serverFormIndex {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("> ")
			if i != 7 { // Don't show input for toggle
				displayValue = m.serverFormInput
			}
		}
		body.WriteString(fmt.Sprintf("%s%s: %s\n", prefix, labels[i], displayValue))
	}
	body.WriteString("\nControls: Enter/Tab (next) | Up/Down (move) | Ctrl+S (save) | Esc (cancel)\n")

	return lipgloss.JoinVertical(lipgloss.Top, title, "", body.String())
}

func (m *MainModel) viewDeleteConfirm() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render("⚠ Delete Server")
	body := fmt.Sprintf("Delete server '%s'?\n\nThis cannot be undone.\n\nPress Y to confirm or N/Esc to cancel.\n", m.deleteServerName)
	return lipgloss.JoinVertical(lipgloss.Top, title, "", body)
}

func (m *MainModel) startServerForm(mode ServerFormMode, originName string, server config.ServerConfig) {
	m.serverFormMode = mode
	m.serverFormOrigin = originName
	m.serverForm = server
	m.serverFormIndex = 0
	m.serverFormInput = m.getServerFormField(0)
	m.uiNotice = ""
	m.screen = ScreenServerForm
}

func (m *MainModel) getServerFormField(index int) string {
	switch index {
	case 0:
		return m.serverForm.Name
	case 1:
		return m.serverForm.Gateway
	case 2:
		return m.serverForm.LocalIP
	case 3:
		return m.serverForm.RemoteIP
	case 4:
		return m.serverForm.LANSubnet
	case 5:
		return strconv.Itoa(m.serverForm.SSHTunnel)
	case 6:
		return m.serverForm.MTU
	case 7:
		return strconv.FormatBool(m.serverForm.FullTunnel)
	case 8:
		return m.serverForm.DNS
	default:
		return ""
	}
}

func (m *MainModel) setServerFormField(index int, value string) error {
	value = strings.TrimSpace(value)
	switch index {
	case 0:
		m.serverForm.Name = value
	case 1:
		m.serverForm.Gateway = value
	case 2:
		m.serverForm.LocalIP = value
	case 3:
		m.serverForm.RemoteIP = value
	case 4:
		m.serverForm.LANSubnet = value
	case 5:
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("SSHTunnel must be a number")
		}
		m.serverForm.SSHTunnel = v
	case 6:
		m.serverForm.MTU = value
	case 7:
		v, err := strconv.ParseBool(strings.ToLower(value))
		if err != nil {
			return fmt.Errorf("FullTunnel must be true or false")
		}
		m.serverForm.FullTunnel = v
	case 8:
		m.serverForm.DNS = value
	}
	return nil
}

func (m *MainModel) moveServerFormField(delta int) {
	if err := m.setServerFormField(m.serverFormIndex, m.serverFormInput); err != nil {
		m.uiNotice = err.Error()
		return
	}
	count := 9
	m.serverFormIndex = (m.serverFormIndex + delta + count) % count
	m.serverFormInput = m.getServerFormField(m.serverFormIndex)
	m.uiNotice = ""
}

func (m *MainModel) saveServerForm() error {
	if err := m.setServerFormField(m.serverFormIndex, m.serverFormInput); err != nil {
		return err
	}
	if err := m.serverForm.Validate(); err != nil {
		return err
	}

	if m.serverFormMode == ServerFormAdd {
		if err := m.configManager.AddServer(m.serverForm); err != nil {
			return err
		}
		m.uiNotice = "Server added"
	} else {
		if err := m.configManager.UpdateServer(m.serverFormOrigin, m.serverForm); err != nil {
			return err
		}
		m.uiNotice = "Server updated"
	}

	servers := m.configManager.GetServers()
	for i := range servers {
		if servers[i].Name == m.serverForm.Name {
			m.serverListIndex = i
			break
		}
	}
	m.screen = ScreenServerSelect
	return nil
}

func (m *MainModel) handleServerFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = ScreenServerSelect
		m.uiNotice = "Changes discarded"
		return m, nil
	case "up":
		m.moveServerFormField(-1)
		return m, nil
	case "down", "enter", "tab":
		// Special handling for FullTunnel toggle on Enter
		if m.serverFormIndex == 7 && msg.String() == "enter" {
			m.serverForm.FullTunnel = !m.serverForm.FullTunnel
			return m, nil
		}
		m.moveServerFormField(1)
		return m, nil
	case " ": // Space bar toggles FullTunnel
		if m.serverFormIndex == 7 {
			m.serverForm.FullTunnel = !m.serverForm.FullTunnel
			return m, nil
		}
		// For other fields, space adds a space character
		m.serverFormInput += " "
		return m, nil
	case "ctrl+s":
		if err := m.saveServerForm(); err != nil {
			m.uiNotice = err.Error()
		}
		return m, nil
	case "backspace":
		// Don't allow backspace on FullTunnel toggle
		if m.serverFormIndex == 7 {
			return m, nil
		}
		r := []rune(m.serverFormInput)
		if len(r) > 0 {
			m.serverFormInput = string(r[:len(r)-1])
		}
		return m, nil
	}

	// Don't allow text input on FullTunnel toggle field
	if msg.Type == tea.KeyRunes && m.serverFormIndex != 7 {
		m.serverFormInput += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m *MainModel) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if err := m.configManager.RemoveServer(m.deleteServerName); err != nil {
			m.uiNotice = err.Error()
		} else {
			m.uiNotice = "Server deleted"
			servers := m.configManager.GetServers()
			if m.serverListIndex >= len(servers) {
				m.serverListIndex = len(servers) - 1
			}
			if m.serverListIndex < 0 {
				m.serverListIndex = 0
			}
		}
		m.screen = ScreenServerSelect
		m.deleteServerName = ""
		return m, nil
	case "n", "esc":
		m.screen = ScreenServerSelect
		m.deleteServerName = ""
		m.uiNotice = "Delete cancelled"
		return m, nil
	}
	return m, nil
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

// ProvisioningMsg is sent when the OTP provisioning request completes.
type ProvisioningMsg struct {
	Server   string
	TunNum   int
	ServerIP string
	ClientIP string
	Error    error
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
const connectMaxRetries = 5
const connectRetryDelay = 2 * time.Second

func isRetryableConnectError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Do not retry auth failures or config errors — they won't succeed on retry.
	if strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "invalid server config") ||
		strings.Contains(msg, "server config not found") ||
		strings.Contains(msg, "failed to load private key") {
		return false
	}
	return true
}

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

		var lastErr error
		for attempt := 1; attempt <= connectMaxRetries; attempt++ {
			if attempt > 1 {
				logging.Infof("connect_retry server=%s attempt=%d/%d", serverName, attempt, connectMaxRetries)
				time.Sleep(connectRetryDelay)
			}
			lastErr = tm.Connect(*server, signer)
			if lastErr == nil {
				return ConnectMsg{Server: serverName}
			}
			logging.Infof("connect_attempt_failed server=%s attempt=%d err=%v", serverName, attempt, lastErr)
			if !isRetryableConnectError(lastErr) {
				return ConnectMsg{Server: serverName, Error: lastErr}
			}
		}

		return ConnectMsg{Server: serverName, Error: fmt.Errorf("connection timed out after %d attempts: %w", connectMaxRetries, lastErr)}
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

func userFacingError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.ToLower(err.Error())
	if strings.Contains(raw, "unable to authenticate") || strings.Contains(raw, "publickey") {
		return "authenticate failed"
	}
	if strings.Contains(raw, "open tun channel failed") {
		return "tunnel channel open failed"
	}
	if strings.Contains(raw, "timed out") {
		return "connection timeout"
	}
	return err.Error()
}

// cmdProvision sends an OTP provisioning request to the control plane server.
func cmdProvision(server config.ServerConfig, keyMgr *keymgmt.KeyManager) tea.Cmd {
	return func() tea.Msg {
		pubKey := keyMgr.GetPublicKeyContent()
		if pubKey == "" {
			return ProvisioningMsg{
				Server: server.Name,
				Error:  fmt.Errorf("SSH public key not found — run 'k' to generate/view key first"),
			}
		}
		// Derive username from OS username (same approach as SSH connection)
		username := os.Getenv("USERNAME")
		if username == "" {
			username = os.Getenv("USER")
		}
		if username == "" {
			username = "vpnuser"
		}

		resp, err := api.Provision(server.ProvisionURL, username, pubKey, server.OTPSharedSecret)
		if err != nil {
			logging.Errorf("provision_failed server=%s err=%v", server.Name, err)
			return ProvisioningMsg{Server: server.Name, Error: err}
		}
		logging.Infof("provision_success server=%s tun=%d", server.Name, resp.TunNum)
		return ProvisioningMsg{
			Server:   server.Name,
			TunNum:   resp.TunNum,
			ServerIP: resp.ServerIP,
			ClientIP: resp.ClientIP,
		}
	}
}

// viewProvisioning renders the OTP provisioning status screen.
func (m *MainModel) viewProvisioning() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		Render("🔐 Provisioning — " + m.selectedServer)

	var body string
	switch {
	case m.provisionError != "":
		body = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("✗ Error: "+m.provisionError) +
			"\n\n[Press Esc or b to go back]"
	case m.provisionResult != "":
		body = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.provisionResult) +
			"\n\n[Press Esc or b to go back]"
	default:
		body = "Sending provisioning request…"
	}

	return title + "\n\n" + body + "\n"
}
