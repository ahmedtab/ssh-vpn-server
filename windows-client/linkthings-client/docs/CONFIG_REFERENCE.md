# Configuration & provisioning reference

Full schema for `ServerConfig` ([config/config.go](../config/config.go)) and the wire format for the
optional OTP-provisioning HTTP flow ([api/provisioning.go](../api/provisioning.go)). The README's
"Configuration Fields" section covers the user-facing subset; this doc adds the provisioning fields
and the exact validation/signing rules for anyone changing either.

## `ServerConfig` fields

Stored as the `servers` array in `%APPDATA%\LinkThings\servers.json`, loaded/saved whole via
`ConfigManager`.

| JSON key | Go field | Type | Required | Default | Validated in `Validate()`? |
|---|---|---|---|---|---|
| `name` | `Name` | string | yes | — | non-empty; also checked for uniqueness by `AddServer`/`UpdateServer` |
| `gateway` | `Gateway` | string (`host:port`) | yes | — | non-empty only (port defaulted to `:22` at connect time in `tunnel_windows.go` if missing, **not** here) |
| `localIP` | `LocalIP` | string (CIDR) | yes | — | non-empty only; CIDR shape is parsed later in `tunnel_windows.go`, not at save time |
| `remoteIP` | `RemoteIP` | string (IP) | yes | — | non-empty only |
| `lanSubnet` | `LANSubnet` | string (CIDR) | yes | — | non-empty only |
| `sshTunnel` | `SSHTunnel` | int | no | `0` | must be `0`–`15` inclusive |
| `mtu` | `MTU` | string | no | `"1340"` | set to default if empty; no numeric/range check |
| `fullTunnel` | `FullTunnel` | bool | no | `true` (only via `createDefaultConfig`; a zero-value `ServerConfig{}` defaults to `false` since Go bools default false — the "defaults to true" behavior only happens where callers explicitly set it, e.g. the `a` add-server key handler) | none |
| `dns` | `DNS` | string, `omitempty` | no | `""` (disabled) | none; comma-separated IPs, parsed by `parseDNSServers` at connect time |
| `provisionURL` | `ProvisionURL` | string, `omitempty` | no | `""` | none — presence alone gates whether the `p` (provision) key is enabled in the UI |
| `otpSharedSecret` | `OTPSharedSecret` | string, `omitempty` | no | `""` | none; expected hex-encoded, see below |

Since `MTU`'s default is applied *inside* `Validate()` (mutates the receiver), calling `Validate()`
on a copy will not persist the default back into the caller's struct unless the caller uses the
mutated copy — `AddServer`/`UpdateServer`/`saveServerForm` all validate the actual struct that then
gets saved, so this works today, but keep it in mind if you refactor validation to be non-mutating
or move it to a `const`/pointer receiver elsewhere.

Adding or removing a field? See the full list of touch points in
[EXTENDING.md](EXTENDING.md#add-a-new-serverconfig-field) — the TUI form has three separate
index-mapped switch statements plus a hardcoded field count that all need updating together.

## Config file lifecycle

- Path: `%APPDATA%\LinkThings\servers.json` (or `~/AppData/Roaming/LinkThings/servers.json` if
  `APPDATA` is unset, e.g. when developing on Linux).
- First run: `Load()` sees no file and calls `createDefaultConfig()`, which writes one default
  server (`Production` example pointed at `157.180.4.166:2255`) and immediately `Save()`s it.
- Every `AddServer`/`UpdateServer`/`RemoveServer` call re-serializes and overwrites the *entire*
  file (`json.MarshalIndent`, 2-space indent) — there's no partial-write/merge path and no file
  locking, so concurrent instances of the app would race on this file (not a concern today since
  the app is single-instance-per-user in practice, but relevant if you ever add multi-window/daemon
  support).

## Provisioning HTTP flow

Optional: only triggered by the `p` key in the TUI, and only if both `ProvisionURL` and
`OTPSharedSecret` are set on the selected server. Talks to a **separate control-plane server** this
repo has no code visibility into — the request/response shapes and signing scheme below must stay
byte-for-byte compatible with that server's `otptoken.Validate` implementation.

### Request (`POST <ProvisionURL>`, JSON body)

```json
{
  "username":  "<OS username>",
  "public_key": "<SSH public key file content, trimmed>",
  "timestamp": "<unix seconds, as a decimal string>",
  "nonce":     "<16 random bytes, hex-encoded (32 hex chars)>",
  "signature": "<hex-encoded HMAC-SHA256, see below>"
}
```

- `username`: `os.Getenv("USERNAME")` (Windows), else `os.Getenv("USER")`, else the literal
  fallback `"vpnuser"`.
- `public_key`: `KeyManager.GetPublicKeyContent()` — the full `ssh-ed25519 AAAA... ` line, not just
  the base64 blob. If the key file can't be read, provisioning fails client-side before any HTTP
  call is made (`"SSH public key not found — run 'k' to generate/view key first"`).
- `signature`: `hex(HMAC-SHA256(secretBytes, message))` where
  `message = username + "|" + public_key + "|" + timestamp + "|" + nonce` (literal `|` separators,
  in that exact order).
- `secretBytes`: `OTPSharedSecret` is **hex-decoded** first; if that fails (not valid hex), the code
  silently falls back to using the raw string's bytes as the HMAC key instead of erroring. This
  means a misconfigured (non-hex) secret won't surface as a client-side error — it will just
  produce a signature the server rejects. Generate secrets with `openssl rand -hex 32` to avoid
  this path entirely.

### Response

`200 OK` body:

```json
{ "username": "...", "tun_num": 0, "server_ip": "...", "client_ip": "..." }
```

Any non-200 status is treated as an error; the client looks for an `"error"` string field in the
JSON body to surface as the message, falling back to a generic
`"provisioning rejected by server"` if absent or unparsable.

Note the response does **not** currently feed back into `ServerConfig` automatically (e.g. to
auto-fill `SSHTunnel`/`RemoteIP`/`LocalIP` from `tun_num`/`server_ip`/`client_ip`) — the TUI just
displays the values on ScreenProvisioning; the user must still transcribe them into the server form
manually if they don't already match.
