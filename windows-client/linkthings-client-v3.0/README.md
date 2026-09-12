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
- Linux (x64), a `pkexec`-capable desktop session for the one-time `CAP_NET_ADMIN` capability grant
  (see "Debugging" below - the app never runs as root on Linux); on Linux you additionally need:
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
- **Elevation model differs by platform** - `main.go` checks `el.IsAdmin()` before creating any
  window and self-relaunches (`RelaunchElevated()`) if not. On **Windows** this is a full UAC prompt
  elevating the whole process, every launch, same as before. On **Linux** it's a one-time `pkexec`
  prompt that grants the binary the `CAP_NET_ADMIN` file capability and re-execs itself
  *unprivileged* - the app never runs as root there (see "Known operational caveat" in
  [CLAUDE.md](CLAUDE.md) for why). A non-interactive shell can't satisfy either prompt - don't try to
  bypass it. On Linux specifically, don't bypass it by running the binary directly under `sudo`
  either: that makes the whole process genuinely root again (root has every capability, including
  `CAP_NET_ADMIN`, by default, so `IsAdmin()` returns true immediately and the capability-grant path
  never runs) - reintroducing the exact tray/`SingleInstance` D-Bus problem this design exists to
  avoid. Let the app manage its own elevation.
- **Rebuilding the binary re-triggers the one-time Linux prompt**: `go build` produces a new inode,
  and file capabilities live on that specific inode's extended attributes - a freshly built binary
  starts without the capability every time. Expected during development, not a regression. Skip it
  entirely with `sudo setcap cap_net_admin+ep ./client-v3` right after each build, or once per
  packaged install (see CLAUDE.md's packaging note).
- **A profile with custom DNS servers may show a *second*, separate password prompt** on connect,
  even after the one-time capability grant - `resolvectl` (DNS configuration) is gated by polkit, not
  capabilities, and only genuine root is exempt from that check. This is an accepted, deliberate
  limitation, not a bug - see CLAUDE.md's operational caveat for the full explanation and why it
  wasn't worth building a separate always-root helper process just to avoid it.
- **Launching the app twice**: the second launch is blocked via Wails' `SingleInstance` guard
  (`main.go`), which brings the already-running instance's window to the front instead of opening a
  second one. Once the capability has been granted at least once, this happens with **no elevation
  prompt at all** for the second launch (the `IsAdmin()` check is now a fast local capability check,
  not a relaunch) - an improvement over the old whole-process-elevation model, where a second launch
  needed a full UAC/pkexec round-trip before being blocked.
- **Frontend-only iteration without the Go elevation gate**: run `npm run dev` directly inside
  `frontend/` to get Vite's dev server against whatever bindings were last generated, without
  triggering a Go rebuild or the elevation prompt. Useful for pure UI/CSS work, but bindings won't
  reflect uncommitted Go service changes until you rerun `wails3 generate bindings -ts ./...`.
