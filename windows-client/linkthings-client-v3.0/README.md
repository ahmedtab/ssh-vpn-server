# LinkThings Client v3 - Desktop GUI SSH VPN Client

A Wails v3 + Vue 3 + Tailwind desktop GUI for the LinkThings SSH VPN, with a system tray and
background-run behavior. This is the actively developed successor to the Bubble Tea TUI in
`linkthings-client-v2/` - see the parent repository's `CLAUDE.md` for the fork rationale, and
[CLAUDE.md](CLAUDE.md) in this directory for the full architecture writeup (what changed vs. v2,
service layer, design source of truth, known caveats).

## Requirements

- Go 1.25+
- Node.js + npm (frontend is Vue 3 + Vite + Tailwind v4)
- The [Wails v3 CLI](https://v3.wails.io/) (`wails3`) - see [Installing the Wails v3 CLI](#installing-the-wails-v3-cli) below
- Windows 10/11 (x64), administrator privileges - **or** -
- Linux (x64), root or an elevation path (`pkexec`-capable desktop session); on Linux you additionally need:
  - Ubuntu 24.04+/Debian 13+: `libgtk-4-dev` + `libwebkitgtk-6.0-dev`
  - Older distros: see the GTK3 fallback build tag at https://v3.wails.io/quick-start/installation

There is no `*_test.go` suite. Verification is `go vet`/`gofmt`/`go build` (both `GOOS=linux` and
`GOOS=windows`), `npm run build` for the frontend, and - since a real (non-stub) tunnel
implementation exists for both platforms - actually running the app to exercise Connect/Disconnect.

## Installing the Wails v3 CLI

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

This installs the binary to `$(go env GOPATH)/bin` (usually `~/go/bin`), which is **not** on `PATH`
by default. Add it once:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc   # or ~/.zshrc
source ~/.bashrc
```

Without this, `wails3` commands (and `wails3 dev`'s own internal re-invocation of `wails3 build`)
will fail with `command not found` / `exit status 127`.

## Starting the app (dev mode)

```bash
cd linkthings-client-v3.0
wails3 dev
```

This runs the full Task pipeline: `go mod tidy` -> generate icons/`.desktop` file -> `npm install` ->
regenerate Go->JS bindings -> build the frontend in dev mode -> `go build` -> launch the binary ->
start Vite in watch mode for hot-reloading the frontend. Changed Go files trigger an automatic
rebuild+relaunch; changed frontend files hot-reload without a Go rebuild.

All of this is driven by the root [Taskfile.yml](Taskfile.yml), which includes
[build/Taskfile.yml](build/Taskfile.yml) (common tasks) plus a per-OS Taskfile under `build/<os>/`.
Run `wails3 task --help` to list every available task (build/package/run per OS, docker/server-mode
variants, signing, etc.).

## Building

```bash
# Native build for the current OS (debug, unoptimized)
wails3 build DEV=true

# Production build for the current OS (stripped, trimmed)
wails3 build

# Cross-compile to Windows from Linux/macOS
GOOS=windows wails3 build
# or, without going through Wails/Task at all (matches v2's build.bat convention):
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/linkthings.win.amd64.exe .
```

Output binaries land in `bin/` (gitignored). `wails3 package` produces installable
packages (AppImage/deb/rpm on Linux, NSIS/MSIX on Windows) via the same per-OS Taskfiles.

### Regenerating bindings

After changing any `services/*.go` method signature, or adding a new service:

```bash
wails3 generate bindings -ts ./...
```

Run this **from `linkthings-client-v3.0/`**, not the repo root - it writes to
`frontend/bindings/` (gitignored, reproducible) relative to the current directory. Running it from
the wrong directory silently writes a bogus `frontend/bindings/` at whatever `cwd` was instead, and
warns `pattern ./...: directory prefix . does not contain main module`.

## Debugging

- **Logs**: written to `%APPDATA%\LinkThings\logs\client.log` on Windows / the Linux equivalent via
  `paths/`, plain `INFO`/`ERROR` lines (see `logging/logger.go`). The Logs screen in the app also
  tails this file live via `LogService`.
- **`wails3 doctor`**: reports missing system dependencies (GTK/WebKitGTK versions, Node, etc.) -
  run this first if `wails3 dev`/`build` fails for an unclear reason.
- **Elevation is required for the whole app**, not just the tunnel - `main.go` checks
  `el.IsAdmin()` before creating any window and self-relaunches elevated
  (`RelaunchElevated()`) if not. On Windows this is a UAC prompt; on Linux it's `pkexec`
  (`elevation_linux.go`). A non-interactive shell can't satisfy this prompt - don't try to bypass
  it (e.g. by running as root directly, which skips real-world elevation behavior you actually want
  to test).
- **`Gtk-WARNING: Failed to open display` after the pkexec prompt succeeds (Linux)**: this is a
  known caveat, not a bug in this app - see "Known operational caveat" in [CLAUDE.md](CLAUDE.md).
  `pkexec` re-execs the process as root, but root usually can't authenticate to your X11/Wayland
  session even with `DISPLAY`/`XAUTHORITY` inherited, because your `Xauthority` file is normally
  `0600`-owned by your user (root can't read the cookie). Practical workarounds for local dev
  (**do not use in production - this loosens X server access control**):
  ```bash
  # Allow the root user to connect to your X server for this login session:
  xhost +si:localuser:root
  wails3 dev
  # Revoke afterwards:
  xhost -si:localuser:root
  ```
  If you're on Wayland (`XDG_SESSION_TYPE=wayland`), the equivalent friction exists via the
  Wayland socket instead of Xauthority; there's no established one-liner workaround, and the two
  real fixes are the ones CLAUDE.md lists: pass `DISPLAY`/`XAUTHORITY`/`WAYLAND_DISPLAY`/
  `XDG_RUNTIME_DIR` through explicitly in `RelaunchElevated`, or move to a privileged-helper model
  where only tunnel/adapter/route operations run elevated and the window stays unprivileged.
- **Frontend-only iteration without the Go elevation gate**: run `npm run dev` directly inside
  `frontend/` to get Vite's dev server against whatever bindings were last generated, without
  triggering a Go rebuild or the elevation prompt. Useful for pure UI/CSS work, but bindings won't
  reflect uncommitted Go service changes until you rerun `wails3 generate bindings -ts ./...`.
