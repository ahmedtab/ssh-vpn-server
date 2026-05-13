# LinkThings Client - SSH VPN Windows Client

A minimal, user-friendly SSH VPN client for Windows with TUI interface, multi-server configuration, and automatic SSH key management.

## Features

- **TUI Interface**: Text User Interface for easy server selection and connection management
- **Multi-Server Support**: JSON-based configuration for managing multiple VPN servers
- **Automatic Key Management**: SSH key generation on first run
- **Single Key Strategy**: Uses one SSH key for both gateway and VPN authentication
- **Windows Integration**: Native Wintun TUN adapter for VPN tunneling
- **Admin Privilege Check**: Automatically validates administrator requirements

## Requirements

- Windows 10/11 (x64/amd64)
- Administrator privileges
- Go 1.22+ (for building from source)

## Installation

### From Binary

1. Download the latest release: `linkthings-client.win.amd64.exe`
2. Run the executable with administrator privileges
3. On first run, SSH keys will be generated automatically in `~\.ssh\`

### Building from Source

#### On Windows (Recommended)

```batch
cd windows-client\linkthings-client
build.bat
```

Output: `dist\linkthings-client.win.amd64.exe`

#### Cross-compiling from Ubuntu/Linux

```bash
cd windows-client/linkthings-client
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o dist/linkthings-client.win.amd64.exe .
```

## Configuration

Server configuration is stored in: `%APPDATA%\LinkThings\servers.json`

### Example Configuration

```json
{
  "servers": [
    {
      "name": "Production-DC1",
      "gateway": "157.180.4.166:2255",
      "localIP": "10.10.11.1/30",
      "remoteIP": "10.10.11.2",
      "lanSubnet": "192.168.4.0/24",
      "sshTunnel": 0,
      "mtu": "1340",
      "fullTunnel": true
    },
    {
      "name": "Staging-DC2",
      "gateway": "staging.example.com:2255",
      "localIP": "10.10.11.1/30",
      "remoteIP": "10.10.11.2",
      "lanSubnet": "192.168.5.0/24",
      "sshTunnel": 1,
      "mtu": "1500",
      "fullTunnel": false
    }
  ]
}
```

### Configuration Fields

- **name**: Display name for the server (used in UI)
- **gateway**: SSH gateway address in `host:port` format
- **localIP**: Local TUN adapter IP with CIDR notation (e.g., `10.10.11.1/30`)
- **remoteIP**: Remote tunnel endpoint IP (e.g., `10.10.11.2`)
- **lanSubnet**: LAN subnet to route through the tunnel (e.g., `192.168.4.0/24`)
- **sshTunnel**: Tunnel slot number (0-15, default 0 for multi-user support) — use different slots for different users
- **mtu**: MTU value in bytes (default `1340`, adjust for your network)
- **fullTunnel**: Route all internet through VPN (default `true`). When enabled, `curl ifconfig.me` returns server IP. When `false`, only LAN subnet routes through tunnel (split tunnel)

## Usage

1. Run the application with administrator privileges
2. Select a server from the list using arrow keys (↑↓)
3. Press Enter to connect
4. View connection logs
5. Press D to disconnect
6. Press S to switch servers
7. Press Q to exit

## SSH Key Management

- **Private Key Location**: `~\.ssh\linkthings_key`
- **Public Key Location**: `~\.ssh\linkthings_key.pub`
- **Key Type**: Ed25519 (256-bit elliptic curve)
- **First Run**: Keys are automatically generated if they don't exist

### Setting Up Your Public Key on the Server

1. Run the app and press **K** from the server selection screen
2. The Setup screen displays your public key with auto-copy to clipboard
3. On the server, add the following line to `~/.ssh/authorized_keys`:
   ```
   tunnel="0",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...
   ```
   (Adjust tunnel slot number if using multiple users)
4. Ensure `sshd_config` contains: `PermitTunnel point-to-point`

## Architecture

### Project Structure

```
linkthings-client/
├── config/
│   └── config.go          # Server configuration management
├── keymgmt/
│   └── keymgmt.go         # SSH key generation and management
├── tunnel/
│   └── tunnel.go          # VPN tunnel implementation
├── ui/
│   ├── model.go           # TUI model and screens
│   └── styles.go          # UI styling utilities
├── main.go                # Application entry point
├── go.mod                 # Go module definition
├── go.sum                 # Go module checksums
├── build.bat              # Windows build script
├── versioninfo.json       # Windows version metadata
├── LICENSE                # MIT License
└── README.md              # This file
```

### Core Components

- **config**: JSON configuration file loading and management
- **keymgmt**: SSH key pair generation, storage, and retrieval
- **tunnel**: Wintun adapter creation, SSH channel establishment, packet bridging
- **ui**: Bubble Tea TUI framework for user interaction

## Security Considerations

- The application requires administrator privileges for network adapter creation
- SSH private keys are stored with restricted permissions (0600)
- Single SSH key is used for both gateway and VPN authentication (as per design)
- Connection logs are displayed in memory only (not persisted)

## Troubleshooting

### "Administrator privileges required"
- Ensure you're running the application with administrator rights (Run as Administrator)

### "Failed to create adapter"
- Check that you have administrator privileges
- Verify Wintun is properly installed (included with WireGuard for Windows)
- Ensure no other adapter with the same name exists

### "Failed to connect to gateway"
- Verify the gateway address and port are correct
- Check network connectivity to the gateway
- Ensure SSH credentials (keys) are valid

### "Configuration not found"
- The application will create a default configuration on first run
- Edit `%APPDATA%\LinkThings\servers.json` to add your servers

## Development

### Building for Development

```bash
go run main.go
```

### Dependencies

- `golang.org/x/crypto` - SSH protocol implementation
- `golang.zx2c4.com/wintun` - Wintun TUN adapter
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - TUI styling

### Running Tests

```bash
go test ./...
```

## License

MIT License - See LICENSE file for details

### Attribution

This project uses:
- **Wintun** (https://www.wintun.net/) - MIT License
- **Bubble Tea** (https://github.com/charmbracelet/bubbletea) - MIT License
- **Go** standard libraries

## Support

For issues, questions, or contributions, please refer to the main project repository.

## Version

Current Version: 1.0.0
Built with: Go 1.22+

---

**Important**: This is a security-sensitive application. Always run from trusted sources and keep your SSH keys private.
