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

## Add Windows-only tunnel/networking behavior

`tunnel_windows.go` and `tunnel_nonwindows.go` must export the **same function signatures** for
anything called from platform-independent code (`main.go`, `ui/model.go`, `config`, `keymgmt`).
Adding a function only to `tunnel_windows.go` breaks `go build`/`go vet` on Linux/macOS, which is
the only verification available for non-networking code when developing off Windows. Add a stub to
`tunnel_nonwindows.go` that returns an error (matching `Connect`'s stub) or a safe no-op (matching
`CleanupOrphanAdapters`'s stub), whichever is more correct for the new function.

Shared, platform-independent state (counters, flags, anything read by the UI regardless of OS)
belongs in [tunnel/tunnel.go](../tunnel/tunnel.go), not duplicated into both platform files.

## Known constraints & deliberate-looking quirks

- **Single active tunnel, always.** The Wintun adapter name is the hardcoded constant
  `stableAdapterName = "LT-Main"` — there is no per-server adapter. Connecting to a different server
  reuses the same adapter/GUID rather than creating a second one. Supporting simultaneous multi-server
  tunnels would require adapter-per-server naming (`sanitizeAdapterName` already exists for this but
  is currently dead code — nothing calls it) and a `TunnelManager` that tracks multiple
  `activeTunnel`s instead of one `any` slot.
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
- **No test suite exists.** There are no `*_test.go` files anywhere in this repo today; if you add
  tests, `config` and `keymgmt` are the most portable/testable packages (no Windows build tag), and
  `tunnel`'s pure-Go helpers (`ipv4FromCIDR`, `networkFromCIDR`, `maskToString`,
  `deterministicAdapterGUID`) are good candidates despite living in a `//go:build windows` file —
  they'd need extracting to an untagged file to be testable from a non-Windows CI runner.
