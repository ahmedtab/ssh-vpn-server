# Tunnel protocol reference

Precise wire-format and shell-command reference for the SSH tunnel. The connection sequence,
channel-open payload, and packet framing below are implemented once, per-OS, in
[tunnel/connect.go](../tunnel/connect.go) and [tunnel/bridge.go](../tunnel/bridge.go) — the
shell-command specifics (routing, DNS, adapter/device lifecycle) are OS-specific, in
[tunnel/tunnel_windows.go](../tunnel/tunnel_windows.go) and
[tunnel/tunnel_linux.go](../tunnel/tunnel_linux.go), behind the `Platform` interface described in
[EXTENDING.md](EXTENDING.md). This is the part of the codebase where "close enough" breaks interop
with the server side (a separate repo), so treat the details below as a fixed contract, not an
implementation detail to refactor freely — in particular, the packet framing (address-family
header) must stay byte-for-byte identical across every OS.

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
| `4:8` | tunnel unit number (`uint32`) | `ServerConfig.SSHTunnel` (0–255) |

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

On Linux there is no adapter-GUID concept: `linuxPlatform.OpenOrCreateTun` creates a plain
`/dev/net/tun` device named `LT-Main` via the `TUNSETIFF` ioctl (no `TUNSETPERSIST`), so the
interface is torn down by the kernel automatically when its fd closes, including on a crash —
Linux has no equivalent of the Windows orphan-adapter problem this section describes, so
`linuxPlatform.CleanupOrphans` is a much lighter defensive check (`ip link show type tun` / `ip
link delete`) rather than a port of the PowerShell logic above.

## Routing

Windows, LAN route (always added): `route add <lanNetwork> mask <lanMask> <RemoteIP> [if
<tunnelIfIndex>]`, via `ensureRoute`, which treats "object already exists" / "file exists" as
non-fatal and falls back to `route change`, then `route delete` + re-`add`, before giving up.

Linux, LAN route (always added): `ip route replace <lanNetwork>/<prefixLen> via <RemoteIP> dev
LT-Main` — `ip route replace` is natively an idempotent upsert, so there's no retry ladder to port.

Full tunnel (`ServerConfig.FullTunnel == true`) additionally, **in this order** (same order on
every OS — this sequencing lives once in `tunnel/connect.go`, not per-platform):

1. Look up the current default gateway: Windows parses `route print 0.0.0.0`
   (`defaultGateway()`); Linux parses `ip -j route show default` as JSON.
2. Pin a `/32` host route to the SSH gateway's IP via that **original** default gateway — this keeps
   the SSH TCP connection itself off the tunnel's default route so tunneling all traffic can't sever
   its own control channel. Windows: `route add <sshHostIP> mask 255.255.255.255 <origGW>`. Linux:
   `ip route replace <sshHostIP>/32 via <origGW>`.
3. Add two `/1` routes via the tunnel interface: `0.0.0.0/128.0.0.0` and `128.0.0.0/128.0.0.0`
   (Windows, via interface index) or `0.0.0.0/1` and `128.0.0.0/1` (Linux, via device name). This
   is the standard "split-default-route" trick — it wins routing priority over the real `0.0.0.0/0`
   default route without ever touching or replacing it.

Teardown removes the LAN route and, if full tunnel was on, the two `/1` routes — it does **not**
remove the host-pin route or touch the real default route (both are harmless to leave/short-lived),
on either OS.

## DNS

Windows applies `ServerConfig.DNS` via `netsh interface ipv4 set/add dnsservers`, scoped to the
tunnel adapter, and never reverts it on disconnect (the adapter persists with its DNS setting
intact until overwritten by a future connect).

Linux applies it via `resolvectl dns LT-Main <servers>` + `resolvectl domain LT-Main ~.`
(systemd-resolved, route-all-domains through this resolver) and does revert it on disconnect via
`resolvectl revert LT-Main`. If `resolvectl` isn't on `PATH` (no systemd-resolved), this is skipped
entirely with a logged warning — there is no `/etc/resolv.conf`-editing fallback in v1.

DNS (`ServerConfig.DNS`, comma-separated IPs) is applied with
`netsh interface ipv4 set/add dnsservers name=<adapter> ...` **scoped to the tunnel adapter only** —
it never changes DNS on any other interface or the OS-wide resolver order.
