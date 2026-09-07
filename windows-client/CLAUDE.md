# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

This repo is the **Windows client** half of a larger SSH-VPN system (a separate server-side control plane exists elsewhere and is referenced only via its API contract). Two Go modules live here, independently:

- `linkthings-client/` — the real, shipping product. A Windows-only TUI SSH VPN client (module `linkthings.io/client`).
- `poc/` — a standalone, throwaway proof-of-concept (`main.go`, Windows-only build tag) that predates `linkthings-client`. Treat it as reference/scratch code, not something to extend; new work goes in `linkthings-client/`.
- `poc-connect.ps1` — PowerShell launcher that builds/runs `poc/poc.exe`.
- `wintun/` — vendored Wintun driver headers/binaries (`wintun.h`, prebuilt DLLs), not Go source.

All commands below assume you're working in `linkthings-client/` unless noted.

## Commands

```bash
cd linkthings-client

# Run directly (works cross-platform for iterating on the TUI; VPN connect itself is Windows-only)
go run main.go

# Build for Windows from Linux/macOS (cross-compile)
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o dist/linkthings-client.win.amd64.exe .

# Build on Windows (produces dist\linkthings-client.win.amd64.exe, embeds version via -ldflags)
build.bat [version]   # e.g. build.bat 1.2.0.0

# Vet / format
go vet ./...
gofmt -l .
```

There are no `*_test.go` files in this repo currently — there is no test suite to run.

## Architecture

### Platform split via build tags

The tunnel and elevation logic is split with `//go:build windows` / `//go:build !windows` file pairs:

- [tunnel/tunnel_windows.go](linkthings-client/tunnel/tunnel_windows.go) — the real implementation: creates/opens a Wintun adapter, dials SSH, opens an OpenSSH `tun@openssh.com` channel, and bridges IP packets between the Wintun session and the SSH channel in two goroutines (`startBridge`). Also owns Windows routing (`route`/`netsh` shellouts) for LAN and full-tunnel routes, DNS assignment on the tunnel adapter, and orphan-adapter cleanup via PowerShell.
- [tunnel/tunnel_nonwindows.go](linkthings-client/tunnel/tunnel_nonwindows.go) — stub that errors on `Connect`, so the rest of the app (UI, config) can be built/run on Linux/macOS for development even though the VPN itself never functions there.
- [elevation_windows.go](linkthings-client/elevation_windows.go) / [elevation_nonwindows.go](linkthings-client/elevation_nonwindows.go) — admin-privilege check and self-relaunch via `ShellExecuteW`/`runas`.
- Shared, platform-independent state (`Stats`, `TunnelManager` struct/mutex) lives in [tunnel/tunnel.go](linkthings-client/tunnel/tunnel.go); only the `Connect`/`Disconnect` methods are platform-specific.

When touching tunnel behavior, check whether the change belongs in the shared file or needs mirroring into both `_windows.go` and `_nonwindows.go`.

### Tunnel identity and lifecycle

- The adapter uses a **stable name** (`LT-Main`) and a **deterministic GUID** derived by hashing the adapter name (`deterministicAdapterGUID`), so reconnecting reuses the same Windows network adapter instead of accumulating new ones. Adapters are cached in-process (`adapterCache`) and intentionally *not* deleted on disconnect — only orphans from crashed prior runs are cleaned up on startup (`CleanupOrphanAdapters`).
- Full-tunnel mode (`ServerConfig.FullTunnel`) works by pinning a host route to the SSH gateway via the original default gateway, then adding two `/1` routes (`0.0.0.0/1`, `128.0.0.0/1`) via the tunnel — the classic split-default-route trick — instead of touching the real default route.
- The SSH side authenticates as `root` using the single Ed25519 keypair from `keymgmt`, and selects a server-side tun device via a numeric "tunnel slot" (0–15) sent in the channel-open payload; the server is expected to map that slot to `tunN` and apply matching `iptables`/`ip` commands over a second SSH session opened just for setup.

### Config and key management

- [config/config.go](linkthings-client/config/config.go): server list persisted as JSON at `%APPDATA%\LinkThings\servers.json`. `ServerConfig.Validate()` is called on every add/update and is the single source of truth for required fields and defaults (e.g. MTU default, tunnel slot range 0–15).
- [keymgmt/keymgmt.go](linkthings-client/keymgmt/keymgmt.go): one Ed25519 keypair per machine at `~/.ssh/linkthings_key(.pub)`, generated on first run. The same key authenticates both the SSH gateway login and (once installed in `authorized_keys` with a `tunnel="N"` restriction) the VPN tunnel channel.
- [api/provisioning.go](linkthings-client/api/provisioning.go): optional HTTP flow to auto-register the public key with a separate control-plane server (`ProvisionURL`/`OTPSharedSecret` fields on `ServerConfig`). Requests are HMAC-SHA256 signed over `username|pubkey|timestamp|nonce`; the signing scheme must stay in lockstep with the server's `otptoken.Validate` — this repo has no visibility into that server code, so don't assume the message format is negotiable unilaterally.

### UI (Bubble Tea TUI)

[ui/model.go](linkthings-client/ui/model.go) is a single Elm-architecture `MainModel` driving all screens via a `Screen` enum (`ScreenServerSelect`, `ScreenConnecting`, `ScreenConnected`, `ScreenDisconnecting`, `ScreenError`, `ScreenSetup`, `ScreenServerForm`, `ScreenDeleteConfirm`, `ScreenProvisioning`). Screen transitions happen in `Update`/`handleKeyPress`; each screen has a matching `view*()` renderer. Long-running work (connect, disconnect, provisioning, stats polling) is dispatched as `tea.Cmd`s that return typed messages (`ConnectMsg`, `DisconnectMsg`, `ProvisioningMsg`, `statsMsg`, ...) back into `Update` — follow this pattern rather than blocking inside `Update` or `View`.

### Logging

[logging/logger.go](linkthings-client/logging/logger.go) writes to `%APPDATA%\LinkThings\logs\client.log` (plain `INFO`/`ERROR` lines). It's a package-level singleton (`Init`/`Close` in `main.go`); use `logging.Infof`/`logging.Errorf`, don't create a second logger.

## Notes for changes

- Any change to `ServerConfig` fields should be reflected in both `Validate()` and the example config in [linkthings-client/README.md](linkthings-client/README.md) — the README documents the on-disk JSON schema.
- Windows-specific networking changes (routes, adapter, DNS) can only be exercised on Windows; when developing on Linux, `go build`/`go vet` for the `!windows` files and cross-compiling with `GOOS=windows` is the available verification, not a real run.

## Deeper reference docs

For extending v1 (not a rewrite), the docs below go past this file's overview into exact wire
formats, shell commands, and file-by-file touch points — read the relevant one before changing that
area rather than re-deriving it from source each time:

- [linkthings-client/docs/PROTOCOL.md](linkthings-client/docs/PROTOCOL.md) — the SSH tunnel wire
  protocol: channel-open payload bytes, packet framing, the exact server-side setup/teardown shell
  commands, adapter-GUID derivation, and the full-tunnel routing sequence.
- [linkthings-client/docs/TUI_REFERENCE.md](linkthings-client/docs/TUI_REFERENCE.md) — the screen
  state machine, full keybinding table, the `tea.Cmd`/`Msg` pattern used for all async work, and the
  server-form field index mapping.
- [linkthings-client/docs/CONFIG_REFERENCE.md](linkthings-client/docs/CONFIG_REFERENCE.md) — full
  `ServerConfig` field table (including the provisioning fields the README omits) and the OTP
  provisioning HTTP request/response/signing format.
- [linkthings-client/docs/EXTENDING.md](linkthings-client/docs/EXTENDING.md) — step-by-step recipes
  (add a config field, add a screen, add a background command, add Windows-only tunnel behavior) and
  a list of known constraints/quirks (single active tunnel, no SSH host-key verification,
  cancel-during-connect is UI-only, etc.) worth knowing before you touch that code.
