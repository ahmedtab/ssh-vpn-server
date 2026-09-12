# Tailwind mapping — LinkThings client (v3 vertical)

The mockup (`LinkThings Client v3 vertical.dc.html`) uses inline styles because of the
preview runtime. In the Vue 3 app, reproduce it with the classes below and the theme in
`tailwind.config.js` — no arbitrary hex values, no ad-hoc `[…]` utilities except where noted.

## Global

- Root shell: `w-[412px] rounded-lg overflow-hidden bg-bg text-ink font-sans shadow-edge`
- Body backdrop: `bg-[#101220]` (desk color, outside the app window — the only literal)
- Focus ring everywhere: `focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2`

## Recurring patterns

| Mock element | Tailwind |
|---|---|
| Card surface | `rounded-[11px] bg-surface shadow-edge p-3.5` |
| Sunken block (key / logs) | `rounded-md bg-sunken ring-1 ring-line` |
| Elevated overlay (profile popover, modals) | `bg-surface-raised shadow-lg rounded-lg` |
| Section label | `text-[10px] uppercase tracking-[0.08em] text-faint` |
| Mono value | `font-mono text-[10.5px] text-faint` |
| Row rule (fading) | `bg-[linear-gradient(to_right,transparent,rgba(233,233,237,.07)_20px,rgba(233,233,237,.07)_calc(100%-20px),transparent)] bg-bottom bg-no-repeat [background-size:100%_1px]` |
| Primary button (outlined) | `inline-flex items-center gap-1.5 rounded-md border border-accent bg-accent/10 px-3 py-1.5 text-accent-300 hover:bg-accent/20 active:bg-accent/25` |
| Secondary button | `… border border-line text-ink hover:bg-ink/5 active:bg-ink/10` |
| Danger button | `… border border-danger/40 text-danger hover:bg-danger/10` |
| Input | `w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] caret-accent hover:border-ink/40 focus-visible:border-accent` |
| Toggle track / knob | `w-9 h-5 rounded-full` + `bg-accent/35 ring-1 ring-accent` (on) / `bg-ink/10 ring-1 ring-ink/20` (off); knob `w-3.5 h-3.5 rounded-full transition-[left]` |
| Tab bar item (active) | `rounded-md bg-accent/15 text-accent-300` |
| Status dot | `w-1.5 h-1.5 rounded-full` + `bg-accent` / `bg-ghost` / `bg-danger` |
| Connected node glow | `shadow-glow ring-1 ring-accent` |
| Tunnel flow line | `bg-[repeating-linear-gradient(to_bottom,theme(colors.accent.DEFAULT)_0_7px,transparent_7px_16px)] animate-flow-down` |

## Layout rules that must survive the port

- Connect / SSH key / Logs screens share **one fixed body height** (514px); only Profiles may grow.
- Inside a fixed screen, exactly one region scrolls: `flex-1 min-h-0 overflow-y-auto`. Headers
  ("Authorized on" + count, filter row) stay `shrink-0` outside it.
- Profile list on the Profiles page: `max-h-[140px] overflow-y-auto` (≈3 rows, then scroll).
- Profile picker is an **overlay**: `absolute top-[calc(100%+6px)] inset-x-0 z-20` + a full-screen
  click-catcher behind it; it must not push content.
- Register flow is a **two-step modal**: profile choice → username/password → result. Ineligible
  profiles render `opacity-45 cursor-not-allowed` and are non-clickable.

## State colors

| State | Token |
|---|---|
| connected / authorized | `accent` (dot, glow, line) |
| idle | `ghost` |
| busy (connecting, disconnecting, registering) | `accent` + `animate-pulse-soft` |
| error / not authorized | `danger` |

Never flood a large area with `accent` — it is a line, a dot and a glow only (Nocturne rule).
