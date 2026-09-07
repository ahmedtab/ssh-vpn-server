# Tunnel protocol reference

Precise wire-format and shell-command reference for the SSH tunnel implemented in
[tunnel/tunnel_windows.go](../tunnel/tunnel_windows.go). This is the part of the codebase where
"close enough" breaks interop with the server side (a separate repo), so treat the details below
as a fixed contract, not an implementation detail to refactor freely.

## Connection sequence

Per `Connect()`, in order:

1. Resolve `localIP`/`localMask` from `ServerConfig.LocalIP` (CIDR) and `lanRouteIP`/`lanMask` from
   `ServerConfig.LANSubnet` (CIDR).
2. Get-or-create the Wintun adapter (always named `LT-Main`, see "Adapter identity" below), set its
   IPv4 address/mask, MTU, and (optionally) DNS.
3. Start a Wintun session (`ringBytes = 4 MiB`).
4. `ssh.Dial("tcp", gateway, cfg)` — `User: "root"`, `Auth: ssh.PublicKeys(signer)`,
   `HostKeyCallback: ssh.InsecureIgnoreHostKey()` (no host-key verification — see Gotchas in
   [EXTENDING.md](EXTENDING.md)).
5. `sshConn.OpenChannel("tun@openssh.com", payload)` — see "Channel-open payload" below. This is
   OpenSSH's native tun forwarding extension; the server must have `PermitTunnel point-to-point` in
   `sshd_config` and the client's key must carry a `tunnel="N"` restriction in `authorized_keys`
   matching the slot below.
6. Open a **second, ordinary** SSH session (`sshConn.NewSession()`) and run the one-shot server-side
   setup command (see below) that brings `tunN` up on the gateway.
7. Add the LAN route, and the full-tunnel routes if enabled (see "Routing" below).
8. Start the two bridge goroutines (`startBridge`).

Disconnect runs the teardown shell command over a fresh session, closes the tun channel and SSH
connection, and removes the routes added in step 7 — but does **not** delete or reset the Wintun
adapter, and does **not** bring `tunN` down or flush its address on the server.

## Channel-open payload

8 bytes, big-endian, matching OpenSSH's `tun@openssh.com` channel-open extension:

| Bytes | Field | Value in this client |
|---|---|---|
| `0:4` | tunnel mode (`uint32`) | `1` (`tunModePointToPoint`, i.e. `SSH_TUNMODE_POINTOPOINT`) |
| `4:8` | tunnel unit number (`uint32`) | `ServerConfig.SSHTunnel` (0–15) |

The server maps this unit number directly to a `tunN` device (see `serverTunDev` below) — this
numbering is the *only* thing that ties a client's config to a specific device on the gateway, and
must match whatever slot the operator granted in that client's `authorized_keys` line.

## Packet framing on the tun channel

Every read/write on `tunCh` is one OpenSSH tun frame:

```
[0:4]  uint32 big-endian address family — 2 = AF_INET, 10 = AF_INET6
[4:]   raw IP packet bytes (as received from / to be injected into the Wintun session)
```

`startBridge` runs two goroutines:

- **Wintun → SSH**: `session.ReceivePacket()` → sniff the IP version nibble (`pkt[0]>>4`) to pick the
  address family → prepend the 4-byte header → `tunCh.Write(frame)`. Counted as `txBytes`.
- **SSH → Wintun**: `tunCh.Read(buf)` → drop the 4-byte header → `session.AllocateSendPacket` +
  `SendPacket` the remainder into Wintun. Counted as `rxBytes`. Frames of length ≤ 4 (no payload)
  are silently skipped.

Both loops exit as soon as `activeTunnel.closed` is set or the underlying Read/Write errors —
there is no reconnect-in-place; a dropped tunnel requires a fresh `Connect()`.

## Server-side setup command (run once, over a plain SSH session)

Built in `serverCfgCmd` (`tunnel_windows.go`), templated per connect:

```sh
sysctl -w net.ipv4.ip_forward=1 2>/dev/null
iptables -t filter -A FORWARD -i tunN -j ACCEPT 2>/dev/null
iptables -t filter -A FORWARD -o tunN -j ACCEPT 2>/dev/null
ip link set tunN mtu <MTU> 2>/dev/null
ip addr flush dev tunN 2>/dev/null
ip addr add <RemoteIP>/32 peer <LocalIP> dev tunN
ip link set tunN up
```

Where `tunN` = `fmt.Sprintf("tun%d", tunSlot)`, `<RemoteIP>` is `ServerConfig.RemoteIP`, and
`<LocalIP>` is the client's Wintun adapter IP. Failure is logged (`server_tun_config_failed`) but
does **not** abort the connect — a broken server-side setup surfaces later as a dead tunnel, not an
immediate error.

Teardown (`activeTunnel.close`), also over a plain session:

```sh
iptables -t filter -D FORWARD -i tunN -j ACCEPT 2>/dev/null
iptables -t filter -D FORWARD -o tunN -j ACCEPT 2>/dev/null
```

Note this only removes the forward rules — it does not flush the address or bring `tunN` down, so
the device is left configured for a fast reconnect.

## Adapter identity

The Windows Wintun adapter is **always** named `LT-Main` (`stableAdapterName`) — the app supports
exactly one active tunnel at a time, regardless of how many servers are configured; switching
servers reuses the same adapter.

`deterministicAdapterGUID(name)` derives a stable Windows adapter GUID purely from the name so
reconnects don't accumulate new adapters:

```
guid_bytes = MD5("linkthings:wintun:" + strings.ToLower(strings.TrimSpace(name)))[0:16]
// then patched to a valid (version 4, variant 10) UUID:
guid.Data3 = (guid.Data3 & 0x0fff) | 0x4000
guid.Data4[0] = (guid.Data4[0] & 0x3f) | 0x80
```

Adapters are cached per-process in `adapterCache` and are **never deleted on disconnect** — only
`CleanupOrphanAdapters()` (run once at startup) removes adapters left behind by a crashed prior
process, matched by name pattern (`LT-*`, or a disconnected legacy `Local Area Connection*`) and
Wintun driver description.

`sanitizeAdapterName` and `CleanupNamedAdapter` exist in `tunnel_windows.go` but are currently
**dead code** — nothing calls them, because adapter naming is not per-server today. Don't assume
per-server adapter names are active just because the helper exists.

## Routing

LAN route (always added): `route add <lanNetwork> mask <lanMask> <RemoteIP> [if <tunnelIfIndex>]`,
via `ensureRoute`, which treats "object already exists" / "file exists" as non-fatal and falls back
to `route change`, then `route delete` + re-`add`, before giving up.

Full tunnel (`ServerConfig.FullTunnel == true`) additionally, **in this order**:

1. Look up the current default gateway (`defaultGateway()`, parses `route print 0.0.0.0`).
2. Pin a `/32` host route to the SSH gateway's IP via that **original** default gateway — this keeps
   the SSH TCP connection itself off the tunnel's default route so tunneling all traffic can't sever
   its own control channel.
3. Add two `/1` routes via the tunnel's interface index: `0.0.0.0/128.0.0.0` and
   `128.0.0.0/128.0.0.0`. This is the standard "split-default-route" trick — it wins routing
   priority over the real `0.0.0.0/0` default route without ever touching or replacing it.

Teardown removes the LAN route and, if full tunnel was on, the two `/1` routes — it does **not**
remove the host-pin route or touch the real default route (both are harmless to leave/short-lived).

DNS (`ServerConfig.DNS`, comma-separated IPs) is applied with
`netsh interface ipv4 set/add dnsservers name=<adapter> ...` **scoped to the tunnel adapter only** —
it never changes DNS on any other interface or the OS-wide resolver order.
