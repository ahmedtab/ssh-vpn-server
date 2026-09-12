# Configuration reference

Full schema for `ServerConfig` ([config/config.go](../config/config.go)). The README's "Configuration
Fields" section covers the user-facing subset; this doc adds validation rules for anyone changing it.

**v3 change from v2**: the `provisionURL`/`otpSharedSecret` fields and the HTTP OTP-provisioning flow
(`api/provisioning.go`) are removed entirely. Registering this machine's key with a gateway is now
done by [sshauth/sshauth.go](../sshauth/sshauth.go): a direct SSH session to the gateway,
authenticating with a username plus a password or a chosen private key, that appends the restricted
`authorized_keys` line to that account's home directory. See
[services/auth_service.go](../services/auth_service.go) for the Wails-facing binding
(`AuthService.AuthorizeViaSSH`). Unlike the old flow, there's no separate control-plane server
involved and no shared secret to provision — but there's also no persistent, server-verified signal
for "is this profile authorized"; the SSH-key screen's "Authorized this session" list is scoped to
the current app session only, not a durable ledger.

## `ServerConfig` fields

Stored as the `servers` array in `servers.json` under the platform config directory (see
[Config file lifecycle](#config-file-lifecycle) below), loaded/saved whole via
`ConfigManager`.

| JSON key | Go field | Type | Required | Default | Validated in `Validate()`? |
|---|---|---|---|---|---|
| `name` | `Name` | string | yes | — | non-empty; also checked for uniqueness by `AddServer`/`UpdateServer` |
| `gateway` | `Gateway` | string (`host:port`) | yes | — | non-empty only (port defaulted to `:22` at connect time in `tunnel/connect.go` if missing, **not** here) |
| `localIP` | `LocalIP` | string (CIDR) | yes | — | non-empty only; CIDR shape is parsed later in `tunnel/connect.go`, not at save time |
| `remoteIP` | `RemoteIP` | string (IP) | yes | — | non-empty only |
| `lanSubnet` | `LANSubnet` | string (CIDR) | yes | — | non-empty only |
| `sshTunnel` | `SSHTunnel` | int | no | `0` | must be `0`–`255` inclusive |
| `mtu` | `MTU` | string | no | `"1340"` | set to default if empty; no numeric/range check |
| `fullTunnel` | `FullTunnel` | bool | no | `true` (only via `createDefaultConfig`; a zero-value `ServerConfig{}` defaults to `false` since Go bools default false — the "defaults to true" behavior only happens where callers explicitly set it, e.g. the `a` add-server key handler) | none |
| `dns` | `DNS` | string, `omitempty` | no | `""` (disabled) | none; comma-separated IPs, parsed by `parseDNSServers` at connect time. Windows: applied via `netsh`, scoped to the tunnel adapter, never reverted on disconnect. Linux: applied via `resolvectl dns`/`resolvectl domain`, reverted via `resolvectl revert` on disconnect; if `resolvectl` isn't on `PATH` (no systemd-resolved), this is skipped with a logged warning rather than editing `/etc/resolv.conf` — a known v1 limitation. |

Since `MTU`'s default is applied *inside* `Validate()` (mutates the receiver), calling `Validate()`
on a copy will not persist the default back into the caller's struct unless the caller uses the
mutated copy — `AddServer`/`UpdateServer`/`saveServerForm` all validate the actual struct that then
gets saved, so this works today, but keep it in mind if you refactor validation to be non-mutating
or move it to a `const`/pointer receiver elsewhere.

Adding or removing a field? See the full list of touch points in
[EXTENDING.md](EXTENDING.md#add-a-new-serverconfig-field) — the TUI form has three separate
index-mapped switch statements plus a hardcoded field count that all need updating together.

## Config file lifecycle

- Path, resolved by [paths.ConfigDir()](../paths/paths.go):
  - Windows: `%APPDATA%\LinkThings\servers.json` (unchanged from the original client — falls back
    to `~/AppData/Roaming/LinkThings/servers.json` if `APPDATA` is unset).
  - Linux: `$XDG_CONFIG_HOME/linkthings/servers.json`, or `~/.config/linkthings/servers.json` if
    `XDG_CONFIG_HOME` is unset (via the stdlib's `os.UserConfigDir()`).
- Logs (`client.log`), resolved by `paths.StateDir()`: Windows reuses `ConfigDir()` (so logs stay
  at `%APPDATA%\LinkThings\logs`, deliberately not moved to `%LocalAppData%`); Linux uses
  `os.UserCacheDir()`, typically `~/.cache/linkthings/logs`.
- Config/log directories are created `0700` and `servers.json`/`client.log` are written `0600`
  (tightened from the original client's `0755`/`0644` during the v2 port — those bits were largely
  cosmetic on Windows but mean "world-readable" on a real POSIX filesystem).
- First run: `Load()` sees no file and calls `createDefaultConfig()`, which writes one default
  server (`Production` example pointed at `157.180.4.166:2255`) and immediately `Save()`s it.
- Every `AddServer`/`UpdateServer`/`RemoveServer` call re-serializes and overwrites the *entire*
  file (`json.MarshalIndent`, 2-space indent) — there's no partial-write/merge path and no file
  locking, so concurrent instances of the app would race on this file (not a concern today since
  the app is single-instance-per-user in practice, but relevant if you ever add multi-window/daemon
  support).

## SSH-based key authorization (replaces v2's HTTP provisioning)

`sshauth.AuthorizeKey` ([sshauth/sshauth.go](../sshauth/sshauth.go)) dials the profile's `Gateway`
directly over SSH — no separate control-plane server involved — authenticating with a username plus
either a password (`ssh.Password`) or an existing private key file (`ssh.PublicKeys`, optionally
passphrase-protected). On success it runs one remote command that appends this machine's restricted
key line to the authenticated account's `~/.ssh/authorized_keys` if not already present:

```
tunnel="<SSHTunnel>",no-pty,no-agent-forwarding,no-port-forwarding,no-user-rc,no-X11-forwarding <public key line>
```

This is the same restriction string the SSH-key screen displays and the real tunnel connection
expects (see [PROTOCOL.md](PROTOCOL.md)). Because the tunnel itself always authenticates as `root`
(`tunnel/connect.go`), this flow only lands the key where it's needed if the operator authenticates
as `root` here too (or the gateway's `PermitRootLogin`/sudo setup otherwise routes it there) — there
is no `sudo`-relay path for a non-root login account in v3.

Credentials passed to this flow are used for exactly one SSH session and are never written to
`servers.json` or logged. Unlike the old HTTP flow, there's also no persistent, server-verified
"is this profile authorized" signal to poll afterward — the SSH-key screen's "authorized this
session" list is scoped to the current app session, not a durable ledger.
