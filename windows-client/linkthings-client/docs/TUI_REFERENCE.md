# TUI reference

State machine, keybindings, and message flow for [ui/model.go](../ui/model.go) — the single
Elm-architecture `MainModel` that drives the whole app. Read this before adding a screen, a
keybinding, or a background command.

## Screen state machine

```
ScreenServerSelect ──enter──► ScreenConnecting ──ConnectMsg(ok)──► ScreenConnected
      │  │  │  │                    │  ctrl+c/q (cancel, UI-only)         │  d/q
      │  │  │  └─k──► ScreenSetup   └─ConnectMsg(err)──► ScreenError      ▼
      │  │  └─a/e──► ScreenServerForm                                ScreenDisconnecting
      │  └─x──► ScreenDeleteConfirm                                      │ DisconnectMsg
      └─p──► ScreenProvisioning                              ScreenServerSelect (or tea.Quit if q was used)
```

`ScreenError` and `ScreenSetup` both return to `ScreenServerSelect` on `enter`/`q`/`ctrl+c` (error)
or `b`/`esc` (setup). `ScreenServerForm` and `ScreenDeleteConfirm` have their own dedicated key
handlers (`handleServerFormKey`, `handleDeleteConfirmKey`), checked at the top of
`handleKeyPress` before the shared `switch msg.String()`.

## Keybindings by screen

| Screen | Keys | Effect |
|---|---|---|
| ServerSelect | `↑`/`↓` | move `serverListIndex` |
| ServerSelect | `enter` | `cmdConnect` → ScreenConnecting |
| ServerSelect | `a` | open ScreenServerForm in Add mode, prefilled with sane defaults |
| ServerSelect | `e` | open ScreenServerForm in Edit mode for the selected server |
| ServerSelect | `x` | ScreenDeleteConfirm (refuses if only 1 server left) |
| ServerSelect | `k` | show pubkey + `authorized_keys` line on ScreenSetup, copies it to clipboard |
| ServerSelect | `p` | `cmdProvision` → ScreenProvisioning (only if `ProvisionURL` + `OTPSharedSecret` set) |
| ServerSelect | `q`/`ctrl+c` | `tea.Quit` |
| Connecting | `q`/`ctrl+c` | sets `connectCanceled=true`, jumps back to ServerSelect (does **not** actually cancel the in-flight SSH dial — see Gotchas) |
| Connected | `d` | `cmdDisconnect` → ScreenDisconnecting → ScreenServerSelect |
| Connected | `q`/`ctrl+c` | `exitOnDisconnect=true`, `cmdDisconnect` → ScreenDisconnecting → `tea.Quit` |
| Error | `enter`/`q`/`ctrl+c` | back to ServerSelect |
| Setup | `b`/`esc` | back to ServerSelect |
| Provisioning | `b`/`esc` | back to ServerSelect |
| ServerForm | `↑`/`↓`/`tab`/`enter` | move field (also commits current field's text into the struct) |
| ServerForm | *typed runes*/`backspace` | edit `serverFormInput` (current field's staged text) |
| ServerForm | `space` on FullTunnel field | toggle boolean; on any other field, inserts a literal space |
| ServerForm | `ctrl+s` | validate + persist via `ConfigManager.AddServer`/`UpdateServer` |
| ServerForm | `esc` | discard, back to ServerSelect |
| DeleteConfirm | `y` | `ConfigManager.RemoveServer`, back to ServerSelect |
| DeleteConfirm | `n`/`esc` | cancel, back to ServerSelect |

## Message / command flow

Bubble Tea pattern used throughout: a key press (or `Init`) returns a `tea.Cmd`, which is a
`func() tea.Msg` run off the UI goroutine; its returned `Msg` re-enters `Update` on the next loop
tick. **Never do blocking work directly inside `Update` or `View`** — follow one of these four
existing pairs:

| Cmd | Msg | Purpose |
|---|---|---|
| `cmdConnect` | `ConnectMsg{Server, Error}` | runs `TunnelManager.Connect`, retrying up to `connectMaxRetries` (5) every `connectRetryDelay` (2s) unless `isRetryableConnectError` says the failure is permanent (auth failure, bad config, missing key) |
| `cmdDisconnect` | `DisconnectMsg{Error}` | runs `TunnelManager.Disconnect` |
| `cmdProvision` | `ProvisioningMsg{Server, TunNum, ServerIP, ClientIP, Error}` | calls `api.Provision` |
| `cmdStatsTick` | `statsMsg{stats}` | `tea.Tick(1s)`; **self-reschedules** by returning another `cmdStatsTick` from its own `Update` handler, but only while `m.screen` is Connecting/Connected/Disconnecting — it stops ticking on any other screen |
| `cmdCopyToClipboard` | `clipboardMsg{ok}` | pipes text to `clip.exe` (Windows-only in practice) |

`Init()` kicks off the very first `cmdStatsTick` unconditionally, so stats start ticking
immediately even before any connection exists (`TunnelManager.Stats()` just returns a zero value
until connected).

## Cancel-during-connect is cosmetic

Pressing `q`/`ctrl+c` on ScreenConnecting does **not** cancel the goroutine started by
`cmdConnect` — there's no `context.Context` plumbed into `ssh.Dial` or the retry loop. It only:

1. Sets `m.connectCanceled = true` and immediately flips the screen back to ServerSelect.
2. When the stale `ConnectMsg` eventually arrives, the `Update` handler checks `connectCanceled`:
   - success → silently calls `cmdDisconnect` to tear down the tunnel that "shouldn't" exist.
   - failure → the error is logged and dropped; the user never sees it.

If you rework cancellation to be a real cancel, keep this suppress-late-result behavior in mind —
some caller elsewhere may still expect a late `ConnectMsg` to arrive.

## Server form field mapping

The form is index-driven (0–8) across three call sites that **must stay in sync** — see
[EXTENDING.md](EXTENDING.md#add-a-new-serverconfig-field) for the exact list of touch points if you
add a field. Index → field today:

| Index | Field | Notes |
|---|---|---|
| 0 | Name | |
| 1 | Gateway | |
| 2 | LocalIP | |
| 3 | RemoteIP | |
| 4 | LANSubnet | |
| 5 | SSHTunnel | parsed with `strconv.Atoi`; non-numeric input rejected in `setServerFormField` |
| 6 | MTU | free text, no numeric validation here (validated as non-empty only in `Validate()`) |
| 7 | FullTunnel | **special-cased**: not free text — `space` or `enter` toggles it; `getServerFormField`/`setServerFormField` still (de)serialize it as `"true"`/`"false"` strings |
| 8 | DNS | comma-separated, no per-item validation here |

`moveServerFormField` hardcodes `count := 9` for wraparound — a new field means both this constant
and the `labels`/`values` slices in `viewServerForm` must grow together, or the new field silently
won't be reachable/saved.
