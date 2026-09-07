# LinkThings Client v2 - Cross-Platform SSH VPN Client

A minimal, user-friendly SSH VPN client for **Windows and Linux** with a TUI interface,
multi-server configuration, and automatic SSH key management. This is the actively
developed successor to the Windows-only `linkthings-client/` (now frozen as legacy v0.2) -
see the parent repository's `CLAUDE.md` for the fork rationale.

## Features

- **TUI Interface**: Text User Interface for easy server selection and connection management
- **Multi-Server Support**: JSON-based configuration for managing multiple VPN servers
- **Automatic Key Management**: SSH key generation on first run
- **Single Key Strategy**: Uses one SSH key for both gateway and VPN authentication
- **Cross-Platform Tunneling**: Native Wintun TUN adapter on Windows, raw `/dev/net/tun` on Linux
- **Privilege Check**: Automatically validates the elevated privileges required to create a tunnel device and manage routes

## Requirements

- Windows 10/11 (x64/amd64), administrator privileges - **or** -
- Linux (x64/amd64), root or an elevation path (see [Elevation on Linux](#elevation-on-linux) below)
- Go 1.22+ (for building from source)

## Installation

### From Binary

1. Download the latest release for your platform: `linkthings-client.win.amd64.exe` or `linkthings-client.linux.amd64`
2. Run it elevated (Administrator on Windows; root, `sudo`, or a `pkexec`-capable desktop session on Linux)
3. On first run, SSH keys will be generated automatically in `~/.ssh/` (`~\.ssh\` on Windows)

### Building from Source

#### On Windows (Recommended for a Windows build)

```batch
cd windows-client\linkthings-client-v2
build.bat
```

Output: `dist\linkthings-client.win.amd64.exe`

#### On Linux (Recommended for a Linux build)

```bash
cd windows-client/linkthings-client-v2
./build.sh
```

Output: `dist/linkthings-client.linux.amd64`

#### Cross-compiling

```bash
# Windows binary, built from Linux/macOS
GOOS=windows GOARCH=amd64 go build -o dist/linkthings-client.win.amd64.exe .

# Linux binary, built from anywhere (no CGO, no C toolchain needed)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/linkthings-client.linux.amd64 .
```

## Elevation on Linux

Creating the tunnel device and managing routes requires `CAP_NET_ADMIN`. On Linux the app:

1. Checks `os.Geteuid() == 0` (i.e. already running as root).
2. If not root, attempts to re-exec itself via `pkexec` (a graphical polkit prompt) -
   this requires a desktop session with a polkit authentication agent running, and does
   **not** work over a plain SSH/headless terminal.
3. If `pkexec` isn't available, it fails closed with the same "administrator privileges
   required" message as Windows.

For a headless or server deployment, either run under `sudo`, or grant the binary the
capability directly so it never needs elevation at all:

```bash
sudo setcap cap_net_admin+ep /path/to/linkthings-client.linux.amd64
```

(This must be reapplied after every rebuild/reinstall of the binary - `setcap` grants are
tied to the specific file, not the app.)

## Configuration

Server configuration is stored at:
- Windows: `%APPDATA%\LinkThings\servers.json`
- Linux: `$XDG_CONFIG_HOME/linkthings/servers.json` (typically `~/.config/linkthings/servers.json`)

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
      "fullTunnel": true,
      "dns": "8.8.8.8,1.1.1.1"
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
- **sshTunnel**: Tunnel slot number (0-255, default 0 for multi-user support) — use different slots for different users
- **mtu**: MTU value in bytes (default `1340`, adjust for your network)
- **fullTunnel**: Route all internet through VPN (default `true`). When enabled, `curl ifconfig.me` returns server IP. When `false`, only LAN subnet routes through tunnel (split tunnel)
- **dns**: (Optional) Comma-separated DNS server IPs applied only to the tunnel adapter (e.g., `"8.8.8.8,1.1.1.1"`). Leave empty to skip DNS configuration. Does not affect other network interfaces. On Linux this requires `resolvectl` (systemd-resolved) - if it isn't on `PATH`, DNS configuration is skipped with a logged warning rather than falling back to editing `/etc/resolv.conf`.

## Usage

1. Run the application elevated (see [Elevation on Linux](#elevation-on-linux) for the non-Windows path)
2. Select a server from the list using arrow keys (↑↓)
3. Press Enter to connect
4. View connection logs
5. Press D to disconnect
6. Press S to switch servers
7. Press Q to exit

## SSH Key Management

- **Private Key Location**: `~/.ssh/linkthings_key` (`~\.ssh\linkthings_key` on Windows)
- **Public Key Location**: `~/.ssh/linkthings_key.pub`
- **Key Type**: Ed25519 (256-bit elliptic curve)
- **First Run**: Keys are automatically generated if they don't exist

### Setting Up Your Public Key on the Server

1. Run the app and press **K** from the server selection screen
2. The Setup screen displays your public key with auto-copy to clipboard (Linux: requires `wl-copy`, `xclip`, or `xsel` on `PATH` - if none are found, copy the displayed key manually)
3. On the server, add the following line to `~/.ssh/authorized_keys`:
   ```
   tunnel="0",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...
   ```
   (Adjust tunnel slot number if using multiple users)
4. Ensure `sshd_config` contains: `PermitTunnel point-to-point`

## Architecture

### Project Structure

```
linkthings-client-v2/
├── config/
│   └── config.go               # Server configuration management
├── keymgmt/
│   └── keymgmt.go              # SSH key generation and management
├── paths/
│   └── paths.go                # OS-appropriate config/log directories (no build tags)
├── tunnel/
│   ├── platform.go             # Platform/TunDevice/Route interfaces - the OS seam
│   ├── connect.go              # Shared Connect/Disconnect orchestration (all platforms)
│   ├── bridge.go               # Shared packet bridge loop (all platforms)
│   ├── tunnel_common.go        # Shared pure-Go helpers (CIDR parsing, runCommand, ...)
│   ├── tunnel_windows.go       # Windows Platform: Wintun, netsh/route, PowerShell
│   ├── tunnel_linux.go         # Linux Platform: /dev/net/tun, ip, resolvectl
│   ├── tunnel_unsupported.go   # Fallback Platform for any other OS
│   └── tunnel.go               # Shared Stats/TunnelManager state
├── ui/
│   ├── model.go                # TUI model and screens
│   ├── styles.go                # UI styling utilities
│   ├── clipboard.go            # Clipboard interface - the OS seam
│   ├── clipboard_windows.go
│   ├── clipboard_linux.go
│   └── clipboard_unsupported.go
├── elevation.go                 # Elevator interface - the OS seam
├── elevation_windows.go         # UAC self-relaunch via ShellExecuteW
├── elevation_linux.go           # pkexec self-relaunch
├── elevation_unsupported.go     # Fallback for any other OS
├── main.go                      # Application entry point
├── go.mod / go.sum
├── build.bat                    # Windows build script
├── build.sh                     # Linux build script
├── versioninfo.json              # Windows version metadata
├── LICENSE
└── README.md                    # This file
```

### Core Components

- **config**: JSON configuration file loading and management
- **keymgmt**: SSH key pair generation, storage, and retrieval
- **paths**: resolves the config/log directory per OS
- **tunnel**: TUN device creation, SSH channel establishment, packet bridging, routing, DNS - orchestrated once in shared files, with each OS implementing a small `Platform` interface of primitives (see [docs/EXTENDING.md](docs/EXTENDING.md))
- **ui**: Bubble Tea TUI framework for user interaction, plus a small per-OS `Clipboard` interface
- **elevation**: a small per-OS `Elevator` interface for the privilege check/self-relaunch main.go needs

Adding a third OS means implementing `Platform`, `Elevator`, and `Clipboard` in one new
file each - nothing in `connect.go`, `bridge.go`, `main.go`, or `ui/model.go` changes.

## Security Considerations

- The application requires elevated privileges for tunnel device creation and route management
- SSH private keys are stored with restricted permissions (0600)
- Single SSH key is used for both gateway and VPN authentication (as per design)
- Connection logs are displayed in memory only (not persisted)

## Troubleshooting

### "Administrator privileges required" / "This application requires administrator privileges"
- Windows: run as Administrator
- Linux: run under `sudo`, ensure a polkit agent is running for the `pkexec` prompt to appear, or grant `cap_net_admin` directly (see [Elevation on Linux](#elevation-on-linux))

### "Failed to create adapter" / "open/create tunnel device failed"
- Windows: verify Wintun is properly installed (included with WireGuard for Windows); ensure no other adapter with the same name exists
- Linux: verify `/dev/net/tun` exists and is accessible; ensure the process actually has `CAP_NET_ADMIN`

### "Failed to connect to gateway"
- Verify the gateway address and port are correct
- Check network connectivity to the gateway
- Ensure SSH credentials (keys) are valid

### "Configuration not found"
- The application will create a default configuration on first run
- Edit `servers.json` at the path shown in the [Configuration](#configuration) section to add your servers

### DNS isn't applied on Linux
- Requires `resolvectl` (systemd-resolved); on distros without it, DNS configuration is skipped with a logged warning rather than editing `/etc/resolv.conf` - this is a known v1 limitation, not a bug

## Development

### Building for Development

```bash
go run main.go
```

### Dependencies

- `golang.org/x/crypto` - SSH protocol implementation (cross-platform)
- `golang.org/x/sys` - `windows` subpackage for Wintun/Win32 calls, `unix` subpackage for Linux TUN ioctls
- `golang.zx2c4.com/wintun` - Wintun TUN adapter (Windows only)
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - TUI styling

### Running Tests

```bash
go test ./...
```

There is no permanent test suite in this repo today; `go build`/`go vet` for both
`GOOS=windows` and `GOOS=linux`, plus real connect/disconnect cycles on native hardware,
are the available verification.

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
