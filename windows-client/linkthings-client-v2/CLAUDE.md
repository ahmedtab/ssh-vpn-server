# CLAUDE.md

Guidance for working on `linkthings-client-v2` specifically. This is the actively developed,
cross-platform (Windows + Linux) fork of the frozen `linkthings-client/` (legacy v0.2, Windows-only
— see the parent repo's `CLAUDE.md` for why the fork exists). The architecture described here
supersedes the parent file's `Architecture` section for this directory; that section describes the
pre-fork state.

## Commands

```bash
cd linkthings-client-v2

# Run directly (works cross-platform for iterating on the TUI)
go run main.go

# Build for Windows from Linux/macOS (cross-compile, no CGO needed)
GOOS=windows GOARCH=amd64 go build -o dist/linkthings-client.win.amd64.exe .

# Build for Linux natively or cross-compiled (no CGO needed)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/linkthings-client.linux.amd64 .

# Build on Windows (produces dist\linkthings-client.win.amd64.exe, embeds version via -ldflags)
build.bat [version]

# Build on Linux (produces dist/linkthings-client.linux.amd64, embeds version via -ldflags)
./build.sh [version]

# Vet / format
go vet ./...
gofmt -l .
```

There is no permanent `*_test.go` suite in this repo. `go build`/`go vet` for both `GOOS=windows`
and `GOOS=linux`, plus real connect/disconnect cycles on native hardware, are the available
verification for tunnel/routing/DNS changes. A useful trick for exercising the Linux `Platform`
implementation's TUN/route calls for real without needing host root: run a throwaway internal test
inside `unshare --user --map-root-user --net --pid --fork` (grants `CAP_NET_ADMIN` without sudo);
delete the test file afterward since it's not meant to be a permanent addition unless you decide
otherwise.

## Architecture: the `Platform`/`Elevator`/`Clipboard` seam

Unlike the original `linkthings-client` (where `tunnel_windows.go` and `tunnel_nonwindows.go` each
independently reimplemented `Connect`/`Disconnect`, including the ~500 lines of OS-agnostic SSH/
framing/orchestration logic), this fork extracts a small interface per OS-dependent concern. The
orchestration is written **once**, in untagged files, and depends only on the interface — each OS
implements a handful of primitives, not a duplicate of the whole feature.

- [tunnel/platform.go](tunnel/platform.go) — the `Platform` interface (`OpenOrCreateTun`,
  `ConfigureAddress`, `AddRoute`, `DeleteRoute`, `DefaultGateway`, `SetDNS`, `RevertDNS`,
  `CleanupOrphans`) and `TunDevice` (a plain `io.ReadWriteCloser` + `Name()`).
- [tunnel/connect.go](tunnel/connect.go) — `TunnelManager.Connect`/`Disconnect`, the `activeTunnel`
  struct, and `CleanupOrphanAdapters()`. Written once for every OS: dials SSH, opens the
  `tun@openssh.com` channel, runs the server-side setup command, decides what routes to add and in
  what order (including the full-tunnel host-pin + split-default sequence), calls `Platform`
  methods for anything OS-specific.
- [tunnel/bridge.go](tunnel/bridge.go) — the two-goroutine packet bridge between `TunDevice` and the
  SSH channel. Also written once: the 4-byte address-family header framing is a wire contract with
  the server side (see [docs/PROTOCOL.md](docs/PROTOCOL.md)) and must never differ by OS.
- [tunnel/tunnel_windows.go](tunnel/tunnel_windows.go) — `windowsPlatform`: Wintun adapter
  create/open, `netsh`/`route`/PowerShell shellouts, a `wintunDevice` that adapts Wintun's
  poll-based ring-buffer API to a plain blocking `io.ReadWriteCloser`.
- [tunnel/tunnel_linux.go](tunnel/tunnel_linux.go) — `linuxPlatform`: raw `/dev/net/tun` +
  `TUNSETIFF` ioctl, `ip`/`resolvectl` shellouts, a `tunFile` wrapping `*os.File`. **The fd must be
  set non-blocking (`unix.SetNonblock`) before wrapping in `os.File`** — verified empirically that
  without this, `Close()` does not unblock a pending `Read()` (see docs/EXTENDING.md's "Known
  constraints" for how this was tested). Also deliberately never sets `TUNSETPERSIST` — the
  interface dies with the fd, so Linux has no real equivalent of Windows's orphan-adapter problem.
- [tunnel/tunnel_unsupported.go](tunnel/tunnel_unsupported.go) — fallback `Platform` for any OS
  that isn't Windows or Linux; every method fails, which `Connect()` surfaces naturally via
  `OpenOrCreateTun` — there's no separate stub to keep in sync.
- [tunnel/tunnel_common.go](tunnel/tunnel_common.go) — pure-Go helpers shared by every platform
  file (`ipv4FromCIDR`, `networkFromCIDR`, `maskToString`, `parseDNSServers`, `runCommand`).
- [tunnel/tunnel.go](tunnel/tunnel.go) — `Stats`/`TunnelManager`, unchanged from the original.

The same pattern, much smaller, applies to:
- [elevation.go](elevation.go) (`Elevator`: `IsAdmin`/`RelaunchElevated`/
  `ShowElevationRequiredMessage`) with [elevation_windows.go](elevation_windows.go) (UAC via
  `ShellExecuteW`), [elevation_linux.go](elevation_linux.go) (`pkexec` re-exec via `syscall.Exec`),
  [elevation_unsupported.go](elevation_unsupported.go).
- [ui/clipboard.go](ui/clipboard.go) (`Clipboard`: `Copy`) with
  [ui/clipboard_windows.go](ui/clipboard_windows.go) (`clip.exe`),
  [ui/clipboard_linux.go](ui/clipboard_linux.go) (`wl-copy`/`xclip`/`xsel`, first found),
  [ui/clipboard_unsupported.go](ui/clipboard_unsupported.go).

**Adding a third OS** means implementing `Platform` + `Elevator` + `Clipboard` in one new file each
(mirroring the Linux files as the template) and narrowing the three `_unsupported.go` files' build
tags to exclude it. Nothing in `connect.go`, `bridge.go`, `main.go`, or `ui/model.go` changes.

**Changing shared behavior** (a routing-sequence change, a new field that affects connect logic) —
edit `connect.go`/`bridge.go` once; it applies to every OS immediately. **Changing OS-specific
behavior** (a Windows `netsh` quirk, a Linux `resolvectl` fallback) — edit only that OS's
`tunnel_<goos>.go`; the `Platform` method signature is the contract the rest of the app doesn't need
to know changed.

## `paths` package

[paths/paths.go](paths/paths.go) resolves `ConfigDir()`/`StateDir()` per OS with no build tags
(just `runtime.GOOS` branches, since this is directory-string logic, not an OS primitive needing a
`Platform`-style interface). Windows deliberately keeps its original `%APPDATA%\LinkThings` layout
byte-for-byte (including log location) rather than adopting `os.UserCacheDir()`'s
`%LocalAppData%` uniformly — that was a conscious decision to avoid a silent behavior change for
existing Windows installs as a side effect of adding Linux support. Linux uses
`os.UserConfigDir()`/`os.UserCacheDir()` (XDG-aware out of the box).

## Everything else

`config/`, `keymgmt/`, `api/`, and `ui/model.go`'s screen state machine are unchanged from the
original client and are already OS-agnostic — see the parent repo's `CLAUDE.md` and this
directory's `docs/` for their behavior (`docs/CONFIG_REFERENCE.md`, `docs/TUI_REFERENCE.md`,
`docs/EXTENDING.md`, `docs/PROTOCOL.md`), which have been updated in place for v2 rather than
duplicated.
