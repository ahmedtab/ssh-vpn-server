# Handoff: LinkThings Client — desktop UI (Vue 3)

## Overview

A desktop GUI for the **LinkThings Windows SSH-VPN client** (today a Bubble Tea TUI, Go, module
`linkthings.io/client`). The GUI covers the same jobs the TUI does: pick one of several saved
server profiles, bring a single SSH tunnel up/down, edit the profile's `ServerConfig` fields,
read/copy the machine's Ed25519 public key (and the `authorized_keys` line the gateway admin needs),
optionally auto-register that key with the control plane, and read the log file.

Primary design: **v3 vertical** — a narrow (412px) panel-style window with a bottom tab bar.
A wider, horizontal variant (v2, 1000px, icon rail) is included for reference only.

## About the design files

`linkthings-ui.html` is a **design reference created in HTML** — a working prototype of the intended
look and behavior, not production code to copy. The task is to **recreate it in Vue 3** using the
target project's patterns. It is self-contained: open it in any browser, click through all four
tabs, connect/disconnect, open the profile picker, run the register flow. Inspect it with devtools
for exact measurements rather than eyeballing screenshots.

Behavior truth lives in the repo docs, not in this mock: `docs/CONFIG_REFERENCE.md` (ServerConfig
schema, validation, provisioning wire format), `docs/PROTOCOL.md` (SSH channel, routing),
`docs/TUI_REFERENCE.md` (state machine, async command pattern). Where the mock and those docs
disagree, the docs win.

## Fidelity

**High-fidelity.** Final colors, typography, spacing and interactions. Recreate pixel-for-pixel
using the Tailwind theme in `tailwind.config.js` and the class recipes in `TAILWIND_MAPPING.md`.
The design system is **Nocturne**: dark blue-grey ground, single blurple accent used as a line/dot/
glow (never as a large fill), outlined buttons, Inter, 8px radii, compact 0.70× spacing.

## Recommended architecture

Vue 3 + **Wails v2** on top of the existing Go core (Wintun + `golang.org/x/crypto/ssh`) — do not
reimplement tunneling in JS. Suggested bindings the UI calls:

| Binding | Returns | Notes |
|---|---|---|
| `ListProfiles()` | `ServerConfig[]` | reads `%APPDATA%\LinkThings\servers.json` |
| `SaveProfile(cfg)` | `{ok, error}` | server-side `Validate()` is the single source of truth |
| `DeleteProfile(name)` | `{ok, error}` | refuses when only one profile remains |
| `Connect(name)` | `{ok, error}` | one active tunnel at a time |
| `Disconnect()` | `{ok, error}` | routes removed, `LT-Main` adapter kept |
| `Stats()` / stream | `{rx, tx, since}` | 1s tick while connecting/connected/disconnecting |
| `GetPublicKey()` | `{line, fingerprint, path, createdAt}` | `~/.ssh/linkthings_key.pub` |
| `Provision(name, user, pass)` | `{tunNum, serverIP, clientIP, error}` | HMAC-signed control-plane call |
| `TailLog()` | stream of `{time, level, msg}` | `%APPDATA%\LinkThings\logs\client.log` |
| `IsElevated()` | `bool` | admin badge in the footer |

The UI is nearly stateless above these: connection state, the selected profile and dialog state are
the only client-side state.

## Screens / views

App shell (all screens): `412px` wide, `rounded-lg`, `bg-bg`, hairline edge (`shadow-edge`).
Top bar (34–41px): shield mark, "LinkThings", status pill on the right.
Bottom: tab bar (4 items) then a 1-line status strip (dot + context + `↓ rx · ↑ tx`).

**Connect, SSH key and Logs share one fixed body height (514px).** Only Profiles may exceed it.
Inside a fixed screen exactly one region scrolls; headers stay pinned.

### 1. Connect

- **Purpose**: see and change tunnel state.
- **Profile trigger** (`bg-surface`, `shadow-edge`, radius 10px): stack icon, profile name (13px/500),
  mono sub-line `gateway · tunN · full tunnel|split`, caret. Opening it turns its edge accent.
- **Profile picker**: an **overlay** — absolutely positioned 6px under the trigger, full width,
  `bg-surface-raised` + `shadow-lg`, max-height 236px, scrolls; last row is "+ New profile".
  A transparent full-screen catcher behind it closes on outside click. It must never push content.
- **Route chain** (radial-gradient panel `#20233a → #191b29`, edge hairline): three node rows —
  This PC / Gateway / Remote LAN — each a 38px rounded tile + title + mono address + right-side
  label (`local` / `tunN` / `remote`). Between them, 34px-tall vertical connectors: a 2px line with
  end-fades, overlaid by a dashed accent gradient animating downward (`animate-flow-down`),
  opacity 1 connected / 0.4 busy / 0 idle. Connector captions: "ssh channel · tun@openssh.com" and
  "full tunnel" | "remote LAN only".
- **Status block**: headline (18px/500) + one-line explanation (12px, muted).
  States: `No active tunnel` · `Dialing gateway…` · `Tunnel up · hh:mm:ss` · `Tearing down…`.
- **Primary action**: full-width 42px outlined button — Connect (accent) / Cancel / Disconnect
  (neutral edge). Cancel is cosmetic in v1 (matches TUI behavior; see TUI_REFERENCE gotcha).
- **Stats row**: 4 columns — Uptime, In, Out, MTU. `—` when idle, mono 12px.
- **Key alert** (conditional): when the selected profile has not authorized this machine's key,
  a tinted accent strip appears with a "Fix" link to the SSH key tab.

### 2. Profiles

- Header: title, `servers.json` hint, "+ New" outlined button.
- **List card**: `max-h-[140px] overflow-y-auto` — 3 rows visible, scroll from the 4th. Row = state
  dot, name, mono gateway, `tunN`. Selected row: accent tint + accent ring.
- **Form card**, fields in this order (all `ServerConfig`): Name · Gateway `host:port` ·
  Local IP (CIDR) + Remote IP (two columns) · LAN subnet · Slot (0–15, number) + MTU + DNS (three
  columns). Slot is clamped 0–15 on input; MTU defaults to `1340` when blank.
- **Full tunnel** toggle in a sunken strip: 36×20 track, 14px knob, label + one plain-language line
  ("All traffic goes through the tunnel" / "Only the remote LAN goes through the tunnel").
- Provisioning line: plug icon, `Auto-provisioning on|off`, URL (truncated).
- Actions: Save (primary) · Test · Duplicate · Delete (icon buttons), then a toast line
  ("Validated · written to servers.json"). Delete opens a confirm modal; the last profile can't be
  deleted.

### 3. SSH key

- Header: title + one-line explanation.
- **Key card** (fixed): segmented tabs `authorized_keys` / `public key`; a context line
  ("restricted to slot N · for <profile>"); the sunken mono block (the `tunnel="N",restrict,
  port-forwarding ` prefix rendered in `accent-300`, key body in `neutral-300`, `break-all`);
  full-width Copy button that flips to "Copied to clipboard" for 1.8s; then Regenerate · Import ·
  auto-register (paper-plane icon button).
- **Register flow — two modals**:
  1. *"Register this key on…"* — list of all profiles; those with a provisioning URL show
     `slot N` and are clickable, the rest render `manual only` at 45% opacity, `not-allowed`,
     click is a no-op.
  2. *Sign in to register* — Username + Password (masked), a note that credentials are used once
     and not stored, Back and "Register key" (disabled-looking until both fields are filled,
     "Registering…" spinner state ~0.9s).
  Result line under the key card names the profile and echoes **that profile's own** values:
  `tun_num N · server_ip <remoteIP> · client_ip <localIP without /mask>`.
- **"Authorized on" card**: the flexible region — header (label + `n/total`) pinned, rows scroll.
  Row = check-circle (accent) or clock (danger) + profile name + `slot N` / `pending`.
- **Key details card** (fixed, outside the scroll): Fingerprint · Path · Created.

### 4. Logs

- Header: title, filter buttons `All | Info | Error` (active = accent tint + accent border),
  file path in mono.
- Log panel: sunken block, fixed height, scrolls. Each entry: mono 10px time + level
  (`INFO` accent-muted `#8b83b5`, `ERROR` `#d98484`) on line 1, message 11px on line 2.
- New entries are prepended live by app actions (connect, disconnect, save, copy, register).

## Interactions & behavior

- Tab switch closes any open overlay. No route/URL — single window, local state.
- Connect: `idle → connecting` (~1.8s in the mock; real = SSH dial with up to 5 retries, 2s apart,
  only for retryable errors) `→ connected`, stats tick every 1s. Disconnect: `connected →
  disconnecting` (~1.1s) `→ idle`.
- All long work must run off the UI thread and return typed results — mirror the TUI's
  `tea.Cmd`/`Msg` discipline; never block a render.
- Copy actions write to the clipboard and log a line.
- Transitions: toggle knob `left` 150ms; background/edge color changes 150ms; the flow dashes are
  the only continuous animation (0.8s linear infinite) and only while busy/connected.
- Hover: `bg-ink/5` on rows and secondary buttons; accent tint on primary/ghost.
  Focus-visible: 2px accent ring, 2px offset. Never leave browser defaults.

## State

```
screen: 'connection' | 'profiles' | 'keys' | 'logs'
conn:   'idle' | 'connecting' | 'connected' | 'disconnecting'
profiles: ServerConfig[]        sel: number
elapsed, rx, tx                 (tick 1s while conn !== 'idle')
pickerOpen: boolean             confirmDelete: boolean
regStep: null | 'profile' | 'creds'   regPick: ServerConfig | null
regUser, regPass, regBusy, regResult
keyTab: 'authorized_keys' | 'public'  copied: boolean
logFilter: 'All' | 'Info' | 'Error'   logs: {time, level, msg}[]
dirty, toast
```

`ServerConfig` = `{name, gateway, localIP, remoteIP, lanSubnet, sshTunnel, mtu, fullTunnel, dns,
provisionURL, otpSharedSecret}` — see `CONFIG_REFERENCE.md` for required/default/validation rules.

## Design tokens

Ground `#161826` · surface `#1c1e2c` · raised `#232532` · sunken `#12141f` · text `#e9e9ed` ·
muted `#9397ab` · faint `#75798c` · ghost `#595d6c` · hairline `rgba(233,233,237,.08)` ·
danger `#d98484` (the one non-system color, an OKLCH counterpart of the accent, used only for
error levels and destructive actions).

Accent ramp: `#f5f4ff #e7e5fe #d2cefd #b5abfc #968ae0 #796cbf #5d5294 #423a6a #2b2741`, base `#9184d9`.
Neutral ramp: `#f3f5fe … #292b31`. Spacing 2.8 / 5.6 / 8.4 / 11.2 / 16.8 / 22.4px.
Radii 4 / 8 / 14px (cards commonly 10–11px). Shadows: edge `0 0 0 1px #3f424d`,
modal `0 0 0 1px #9397ab, 0 16px 40px rgba(0,0,0,.65)`, node glow `0 0 22px rgba(145,132,217,.32)`.
Type: Inter 400/500 (headings never past 500); mono for every address, key, path and log line.

Full mapping in `TAILWIND_MAPPING.md`; theme in `tailwind.config.js`.

## Assets

- **Icons**: [Phosphor](https://phosphoricons.com) — regular + fill. Used: shield-check, stack, key,
  list-dashes, desktop-tower, hard-drives, buildings, caret-up-down, play, power, circle-notch,
  copy, check, arrows-clockwise, upload-simple, paper-plane-tilt, prohibit, clock-countdown,
  check-circle, plugs, plugs-connected, floppy-disk, trash, plus, x, arrow-left, seal-check.
  Install `@phosphor-icons/vue` rather than the CDN font.
- **Fonts**: Inter (400/500/600) — self-host or `@fontsource/inter`.
- No images. All sample data (profiles, key, log lines) is placeholder.

## Files

- `linkthings-ui.html` — standalone interactive prototype (open in a browser). **Reference only.**
- `tailwind.config.js` — Nocturne tokens as a Tailwind theme.
- `TAILWIND_MAPPING.md` — mock patterns → Tailwind classes + layout invariants.
- `source/LinkThings Client v3 vertical.dc.html` — authoring source of the prototype.
- `source/LinkThings Client v2 horizontal.dc.html` — wide-window variant, reference only.
