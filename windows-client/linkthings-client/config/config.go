package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig represents a VPN server configuration
type ServerConfig struct {
	Name       string `json:"name"`
	Gateway    string `json:"gateway"`    // host:port format
	LocalIP    string `json:"localIP"`    // Local TUN adapter IP (e.g., 10.10.11.1/30)
	RemoteIP   string `json:"remoteIP"`   // Remote tunnel IP (e.g., 10.10.11.2)
	LANSubnet  string `json:"lanSubnet"`  // LAN subnet to route through tunnel (e.g., 192.168.4.0/24)
	SSHTunnel  int    `json:"sshTunnel"`  // Tunnel slot number (0-15, default 0 for multi-user support)
	MTU        string `json:"mtu"`        // MTU value (default "1340")
	FullTunnel bool   `json:"fullTunnel"` // Route all internet through VPN (default true)
}

// Config represents the complete configuration
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

// ConfigManager manages configuration loading and persistence
type ConfigManager struct {
	configPath string
	config     *Config
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() (*ConfigManager, error) {
	cm := &ConfigManager{}
	if err := cm.initConfigPath(); err != nil {
		return nil, err
	}
	return cm, nil
}

// initConfigPath initializes the config file path
func (cm *ConfigManager) initConfigPath() error {
	appDataDir := os.Getenv("APPDATA")
	if appDataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine config directory: %w", err)
		}
		appDataDir = filepath.Join(homeDir, "AppData", "Roaming")
	}

	configDir := filepath.Join(appDataDir, "LinkThings")
	cm.configPath = filepath.Join(configDir, "servers.json")

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return nil
}

// Load loads the configuration from file or creates default if not exists
func (cm *ConfigManager) Load() error {
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		// Create default configuration
		return cm.createDefaultConfig()
	}

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	cm.config = &Config{}
	if err := json.Unmarshal(data, cm.config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// createDefaultConfig creates a default configuration file
func (cm *ConfigManager) createDefaultConfig() error {
	cm.config = &Config{
		Servers: []ServerConfig{
			{
				Name:       "Default Server",
				Gateway:    "157.180.4.166:2255",
				LocalIP:    "10.10.11.1/30",
				RemoteIP:   "10.10.11.2",
				LANSubnet:  "192.168.4.0/24",
				SSHTunnel:  0,
				MTU:        "1340",
				FullTunnel: true,
			},
		},
	}

	return cm.Save()
}

// Save saves the configuration to file
func (cm *ConfigManager) Save() error {
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetServers returns all configured servers
func (cm *ConfigManager) GetServers() []ServerConfig {
	if cm.config == nil {
		return []ServerConfig{}
	}
	return cm.config.Servers
}

// GetServer returns a server by name
func (cm *ConfigManager) GetServer(name string) *ServerConfig {
	if cm.config == nil {
		return nil
	}
	for i := range cm.config.Servers {
		if cm.config.Servers[i].Name == name {
			return &cm.config.Servers[i]
		}
	}
	return nil
}

// AddServer adds a new server configuration
func (cm *ConfigManager) AddServer(server ServerConfig) error {
	if cm.config == nil {
		cm.config = &Config{}
	}

	if err := server.Validate(); err != nil {
		return err
	}

	// Check for duplicate names
	for _, s := range cm.config.Servers {
		if s.Name == server.Name {
			return fmt.Errorf("server with name '%s' already exists", server.Name)
		}
	}

	cm.config.Servers = append(cm.config.Servers, server)
	return cm.Save()
}

// UpdateServer updates an existing server by name.
func (cm *ConfigManager) UpdateServer(oldName string, server ServerConfig) error {
	if cm.config == nil {
		return fmt.Errorf("no servers configured")
	}

	if err := server.Validate(); err != nil {
		return err
	}

	index := -1
	for i := range cm.config.Servers {
		if cm.config.Servers[i].Name == oldName {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("server '%s' not found", oldName)
	}

	for i := range cm.config.Servers {
		if i != index && cm.config.Servers[i].Name == server.Name {
			return fmt.Errorf("server with name '%s' already exists", server.Name)
		}
	}

	cm.config.Servers[index] = server
	return cm.Save()
}

// RemoveServer removes a server by name
func (cm *ConfigManager) RemoveServer(name string) error {
	if cm.config == nil {
		return fmt.Errorf("no servers configured")
	}

	for i, s := range cm.config.Servers {
		if s.Name == name {
			cm.config.Servers = append(cm.config.Servers[:i], cm.config.Servers[i+1:]...)
			return cm.Save()
		}
	}

	return fmt.Errorf("server '%s' not found", name)
}

// ValidateServer validates server configuration
func (s *ServerConfig) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("server name is required")
	}
	if s.Gateway == "" {
		return fmt.Errorf("gateway address is required")
	}
	if s.LocalIP == "" {
		return fmt.Errorf("local IP is required")
	}
	if s.RemoteIP == "" {
		return fmt.Errorf("remote IP is required")
	}
	if s.LANSubnet == "" {
		return fmt.Errorf("LAN subnet is required")
	}
	if s.SSHTunnel < 0 || s.SSHTunnel > 15 {
		return fmt.Errorf("SSH tunnel slot must be 0-15, got %d", s.SSHTunnel)
	}
	if s.MTU == "" {
		s.MTU = "1340" // Set default if not specified
	}
	// fullTunnel defaults to true
	return nil
}

// ConfigPath returns the path to the config file
func (cm *ConfigManager) ConfigPath() string {
	return cm.configPath
}
