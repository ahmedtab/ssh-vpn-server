# Extension recipes & known constraints

Concrete, file-and-line-level recipes for the changes most likely to come up when extending v1, plus
a list of behaviors that look like bugs but are deliberate (or at least currently load-bearing) —
know these before "fixing" them.

## Add a new `ServerConfig` field

Touch every one of these or the field will exist in JSON but be invisible/unreachable in the app:

1. [config/config.go](../config/config.go) — add the field + JSON tag to `ServerConfig`, and add any
   required-ness / default / range check to `Validate()`.
2. [ui/model.go](../ui/model.go) `viewServerForm` — add to both the `labels` and `values` slices
   (same index in each).
3. [ui/model.go](../ui/model.go) `getServerFormField` — add a `case <newIndex>:` returning the
   field's current value as a string.
4. [ui/model.go](../ui/model.go) `setServerFormField` — add a `case <newIndex>:` parsing the staged
   string back into the field (with its own error message on parse failure, matching the pattern
   used for `SSHTunnel`/`FullTunnel`).
5. [ui/model.go](../ui/model.go) `moveServerFormField` — bump the hardcoded `count := 9` to match
   the new total field count.
6. [ui/model.go](../ui/model.go) `handleKeyPress`, case `"a"` — add the field to the default
   `config.ServerConfig{...}` literal used when adding a new server, if it needs a non-zero default.
7. [linkthings-client/README.md](../README.md) — add to the example JSON and the "Configuration
   Fields" list (this is the on-disk schema doc for users).
8. [docs/CONFIG_REFERENCE.md](CONFIG_REFERENCE.md) — add a row to the field table here.

If the field is a boolean that needs toggle-style UI (like `FullTunnel`), also special-case it in
`viewServerForm` (display `[X] ON`/`[ ] OFF` instead of the raw form input) and in
`handleServerFormKey` (`space`/`enter` toggle instead of accepting typed runes) — see the `i == 7`
branches for the existing pattern.

## Add a new screen

1. Add a constant to the `Screen` enum ([ui/model.go](../ui/model.go), near the top).
2. Add a `case ScreenX:` to `View()` calling a new `viewX() string` renderer.
3. Wire input:
   - If the screen needs its own multi-key editing mode (like the server form or delete confirm),
     add a dedicated `handleXKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)` and route to it at the top of
     `handleKeyPress`, the same way `ScreenServerForm`/`ScreenDeleteConfirm` are routed before the
     shared switch.
   - Otherwise, add `if m.screen == ScreenX { ... }` branches inside the existing
     `switch msg.String()` cases (`enter`, `b`/`esc`, `q`/`ctrl+c`, ...) alongside the other
     screens' branches, keeping the "back to ServerSelect" convention consistent.
4. If the screen represents async work in progress (like Connecting/Provisioning), add a `Msg`
   type + `cmdX` per the pattern below, and handle it in `Update()`.

## Add a new background command (never block in `Update`/`View`)

Every piece of async work in this app follows the same shape — copy it rather than inventing a new
pattern:

```go
type FooMsg struct {
    Result string
    Error  error
}

func cmdFoo(arg string) tea.Cmd {
    return func() tea.Msg {
        result, err := doSlowThing(arg)
        return FooMsg{Result: result, Error: err}
    }
}
```

Then in `Update`, add a `case FooMsg:` that updates model state and screen, returning `(m, nil)` or
another `tea.Cmd` if follow-up work is needed. `cmdConnect`, `cmdDisconnect`, `cmdProvision`, and the
self-rescheduling `cmdStatsTick` are the four existing examples — see
[TUI_REFERENCE.md](TUI_REFERENCE.md#message--command-flow) for what each does.

## Add OS-specific tunnel/networking behavior

Unlike the original `linkthings-client`, `Connect`/`Disconnect` and the packet bridge are **not**
per-platform anymore — they live once, untagged, in [tunnel/connect.go](../tunnel/connect.go) and
[tunnel/bridge.go](../tunnel/bridge.go), and depend only on the `Platform`/`TunDevice` interfaces
in [tunnel/platform.go](../tunnel/platform.go). Adding OS-specific behavior means one of:

- **A new OS entirely**: implement `Platform` (8 methods: `OpenOrCreateTun`, `ConfigureAddress`,
  `AddRoute`, `DeleteRoute`, `DefaultGateway`, `SetDNS`, `RevertDNS`, `CleanupOrphans`) plus a
  `TunDevice` (a plain `io.ReadWriteCloser` + `Name()`) in one new `tunnel_<goos>.go` file tagged
  `//go:build <goos>`, and narrow `tunnel_unsupported.go`'s tag to exclude the new OS (it's
  currently `!windows && !linux`). `tunnel_windows.go` and `tunnel_linux.go` are the two reference
  implementations to model a third on.
- **A behavior change that's the same on every OS** (e.g. a new field affecting routing decisions,
  a framing/protocol change): edit `connect.go`/`bridge.go` once. This is the whole point of the
  seam — one change, reviewed once, not N platform files kept in sync by discipline.
- **A behavior change that's genuinely OS-specific** (e.g. a Windows `netsh` quirk, a Linux
  `resolvectl` fallback): edit only that OS's `tunnel_<goos>.go`; the `Platform` interface's method
  signature is the contract the other OSes don't need to know changed.

Shared, platform-independent state (counters, flags, anything read by the UI regardless of OS)
belongs in [tunnel/tunnel.go](../tunnel/tunnel.go) (the `Stats`/`TunnelManager` struct, unchanged
from the original) or [tunnel/connect.go](../tunnel/connect.go) (`activeTunnel`, which used to be
duplicated per-platform and now isn't).

The same pattern is used for `Elevator` ([elevation.go](../elevation.go), repo root, package
`main`: `IsAdmin`/`RelaunchElevated`/`ShowElevationRequiredMessage`) and `Clipboard`
([ui/clipboard.go](../ui/clipboard.go): `Copy`) — both much smaller interfaces, same idea: one
`currentX()` constructor per OS file, nothing else changes.

## Known constraints & deliberate-looking quirks

- **Single active tunnel, always.** The tunnel device name is the hardcoded constant
  `stableAdapterName = "LT-Main"` in [tunnel/connect.go](../tunnel/connect.go), shared across every
  OS — there is no per-server adapter. Connecting to a different server reuses the same
  device/GUID (Windows) or fd (Linux) rather than creating a second one. Supporting simultaneous
  multi-server tunnels would require per-server device naming and a `TunnelManager` that tracks
  multiple `activeTunnel`s instead of one `any` slot. (The original repo's dead-code
  `sanitizeAdapterName`/`CleanupNamedAdapter` helpers for this were removed during the v2 refactor
  since nothing called them — re-add them if you actually build this.)
- **No SSH host-key verification.** `HostKeyCallback: ssh.InsecureIgnoreHostKey()` in
  `TunnelManager.Connect` accepts any gateway host key. This is a real MITM exposure on the initial
  TCP path to the gateway, not an oversight to silently "fix" without considering how a real client
  would pin/verify a host key (there's currently no config field or first-connect-TOFU flow for it).
- **Cancel-during-connect is UI-only**, not a real `context`-based cancel — see
  [TUI_REFERENCE.md](TUI_REFERENCE.md#cancel-during-connect-is-cosmetic). The in-flight goroutine
  keeps dialing/retrying in the background even after the screen has moved on.
- **`FullTunnel`'s "default true" is not enforced by `Validate()`.** It's Go's zero value (`false`)
  unless a caller explicitly sets it — both `createDefaultConfig()` and the `a` (add server) key
  handler do, but any other construction path (e.g. deserializing a hand-edited config missing the
  key, or a future API/import path) will silently get `false`. See
  [CONFIG_REFERENCE.md](CONFIG_REFERENCE.md#servercconfig-fields).
- **The tunnel-slot contract is entirely out-of-band.** Nothing in this repo enforces that
  `ServerConfig.SSHTunnel` matches the `tunnel="N"` restriction in the server's
  `authorized_keys`, or that slot N actually maps to a Linux `tunN` device there — see
  [PROTOCOL.md](PROTOCOL.md#channel-open-payload). Mismatches fail silently/late (wrong device
  gets configured, or the channel open is rejected by OpenSSH) rather than with a clear client-side
  error.
- **Provisioning response isn't wired back into config.** `cmdProvision`'s result
  (`tun_num`/`server_ip`/`client_ip`) is displayed to the user on ScreenProvisioning but not
  auto-applied to the `ServerConfig` being provisioned — see
  [CONFIG_REFERENCE.md](CONFIG_REFERENCE.md#response).
- **No permanent test suite exists.** There are no `*_test.go` files anywhere in this repo today.
  `config`, `keymgmt`, and `paths` are the most portable/testable packages (no build tags); the
  pure-Go tunnel helpers (`ipv4FromCIDR`, `networkFromCIDR`, `maskToString`, `parseDNSServers`,
  `runCommand`) already live in the untagged [tunnel/tunnel_common.go](../tunnel/tunnel_common.go),
  so they're testable without a Windows/Linux-specific build tag. `deterministicAdapterGUID` stays
  in `tunnel_windows.go` (no Linux equivalent). The `Platform` implementations themselves were
  verified once, manually, against a real kernel in an unprivileged `unshare --user --map-root-user
  --net` namespace (which grants `CAP_NET_ADMIN` without needing host root) — that's a reasonable
  way to exercise `tunnel_linux.go`'s TUN/route/DNS calls for real without a live SSH-VPN gateway,
  if you need to re-verify something here later.
- **Linux TUN fds must be set non-blocking before wrapping in `os.File`.** This is not optional
  polish: `tunnel_linux.go`'s `OpenOrCreateTun` calls `unix.SetNonblock(fd, true)` before
  `os.NewFile`, because without it the fd is not integrated with Go's runtime poller and
  `TunDevice.Close()` does **not** unblock a pending `Read()` — the goroutine stays blocked in the
  kernel indefinitely. This was verified empirically (a blocking-mode TUN fd's `Read` survives a
  concurrent `Close` of the same fd number; a non-blocking, poller-integrated one returns "file
  already closed" within milliseconds). If you ever touch `OpenOrCreateTun`, keep this call.
- **Linux never sets `TUNSETPERSIST`.** Unlike a Wintun adapter (a kernel-resident virtual NIC
  independent of the Go process), a plain Linux TUN interface is torn down automatically when its
  fd closes, including on a crash — verified empirically (the interface disappears from `ip link
  show` the moment the owning process's fd is gone). This makes Windows's "orphan adapter from a
  crashed process" class of bug structurally rare on Linux, so `linuxPlatform.CleanupOrphans` is a
  much lighter defensive check than the Windows PowerShell version, not a 1:1 port of it.
- **Linux DNS is `resolvectl`-only, with no `/etc/resolv.conf` fallback.** If `resolvectl` isn't on
  `PATH` (non-systemd-resolved distros), `SetDNS`/`RevertDNS` log a warning and no-op rather than
  erroring the whole connect or editing `/etc/resolv.conf` directly. This was a deliberate v1 scope
  decision, not an oversight — see [CONFIG_REFERENCE.md](CONFIG_REFERENCE.md). Note also that
  `resolvectl` talks to the *host* systemd-resolved over D-Bus, so it cannot see an interface that
  only exists inside an isolated network namespace — the `unshare` trick above verifies TUN/route
  primitives for real, but not the DNS path; that needs a real (non-namespaced) Linux host.
