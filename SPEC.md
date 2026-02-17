# OpenClaw Dashboard — Spec

## Problem
Managing OpenClaw agents through Signal/messaging is functional but blind. You can't:
- See all running agents at a glance
- Watch live streaming logs from multiple agents
- Quickly switch between tasks or kill runaway sessions
- Track spend/tokens across sessions
- Get a spatial overview of what's happening

## Proposal: Hybrid TUI + Web Dashboard

Build **both** — a TUI for power users in the terminal, and a web UI for richer visuals. They share the same backend API.

---

## Architecture

```
┌─────────────────────────────────────────────┐
│           OpenClaw Gateway (existing)        │
│  - Sessions, processes, cron, config, etc.   │
└──────────────────┬──────────────────────────┘
                   │ Internal API
┌──────────────────┴──────────────────────────┐
│         Dashboard Server (new)               │
│  - REST API for session/process queries      │
│  - WebSocket for live log streaming          │
│  - Serves web UI static files                │
│  - Listens on localhost:9090                  │
└──────────┬───────────────┬──────────────────┘
           │               │
    ┌──────┴─────┐  ┌──────┴──────┐
    │  TUI Client │  │  Web Client  │
    │  (Bubble Tea)│  │  (React)     │
    └────────────┘  └─────────────┘
```

### Dashboard Server
- **Language:** Go (or Node — matches OpenClaw's ecosystem)
- **Runs as:** `openclaw dashboard` subcommand or standalone binary
- **Connects to:** OpenClaw gateway via its existing internal mechanisms (reads same config)
- **Exposes:**
  - `GET /api/sessions` — list all sessions with status, cost, runtime
  - `GET /api/sessions/:id/history` — message history
  - `GET /api/processes` — list running exec processes
  - `WS /api/processes/:id/logs` — streaming log tail
  - `GET /api/cron` — cron jobs
  - `POST /api/sessions/:id/send` — inject a message
  - `POST /api/processes/:id/kill` — kill a process
  - `GET /api/config` — current gateway config
  - `GET /api/status` — gateway status + model info

---

## Option 2: TUI Dashboard

**Tech:** Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss)

### Layout

```
┌─ OpenClaw Dashboard ──────────────────────────────────────────────┐
│ Sessions (3 active)                          │ Logs: keen-ocean    │
│ ─────────────────────                        │ ────────────────── │
│ ● keen-ocean    warm-messenger  $0.42  3m    │ Reading index.css  │
│ ● amber-daisy   deploy ffxi    $0.00  1m    │ Updating theme...  │
│ ○ tidy-meadow   vent server    idle   42m   │ Writing ChatArea.. │
│                                              │ ✔ 12/18 files done │
│ Processes (2 running)                        │                    │
│ ─────────────────────                        │                    │
│ ▶ amber-daisy   deploy.sh      1m           │                    │
│ ▶ tidy-meadow   air (go)       42m          │                    │
│                                              │                    │
│ Cron Jobs                                    │                    │
│ ──────────                                   │                    │
│ heartbeat   every 30m   last: 7:06 AM        │                    │
│                                              │                    │
├──────────────────────────────────────────────┤                    │
│ [s]essions [p]rocesses [c]ron [k]ill [q]uit  │ [f]ollow [/]search │
└──────────────────────────────────────────────┴────────────────────┘
```

### Features
- **Left panel:** Session list with live status indicators (●/○), cost, runtime
- **Right panel:** Live-streaming log for selected session (auto-follows)
- **Keyboard driven:**
  - `j/k` or arrows to navigate sessions
  - `Enter` to view session detail/history
  - `l` to show logs for selected process
  - `k` to kill selected process (with confirmation)
  - `m` to send a message to selected session
  - `f` to toggle log auto-follow
  - `/` to search/filter
  - `Tab` to switch between panels
  - `1-3` to switch tabs (sessions/processes/cron)
- **Color coding:** Green = running, yellow = thinking, red = failed, dim = completed
- **Cost tracking:** Running total per session and global

### Nice-to-haves
- Sparkline showing tokens/sec for active sessions
- Git branch indicator per workspace
- Notification popup when a session completes
- Split view — watch 2+ logs simultaneously

---

## Option 3: Web Dashboard

**Tech:** React + Tailwind + WebSocket (or Vite + any framework)

### Pages

**1. Overview (Dashboard)**
- Card grid showing all active sessions
- Each card: name, status badge, model, cost, runtime, last activity
- Clicking a card opens the detail view
- Global stats bar: total sessions, total cost today, active processes

**2. Session Detail**
- Full message history (rendered markdown)
- Live streaming log panel (WebSocket)
- Action buttons: Send message, Kill, Restart
- Token usage chart (input/output over time)
- File changes diff viewer (if workspace attached)

**3. Process Monitor**
- Table of all exec processes with live status
- Click to expand and see streaming output
- Kill button per process
- Filter: running / completed / failed

**4. Branch Previewer** (bonus — perfect for tonight's use case)
- List all UI branches
- "Activate" button that runs `git checkout` + restarts dev server
- Side-by-side screenshot comparison (using headless browser to snap each branch)
- Voting/rating system for comparing designs

**5. Cron Manager**
- List jobs with next run time
- Enable/disable toggle
- Run now button
- Run history with expandable output

**6. Config Editor**
- View current gateway config
- Edit with validation
- Apply with one click (triggers gateway restart)

### Real-time
- WebSocket connection for:
  - Process log streaming
  - Session status updates
  - Cost ticker
  - Completion notifications (toast/sound)

### Visual Design
- Dark theme (of course)
- Monospace for logs, Inter for UI
- Status colors: green/yellow/red/gray
- Compact density option for power users

---

## Implementation Plan

### Phase 1: API Layer (1-2 days)
- Build the dashboard server as an OpenClaw plugin/skill
- REST endpoints wrapping existing OpenClaw internals
- WebSocket log streaming
- Auth: localhost-only by default, optional token for remote

### Phase 2: TUI (2-3 days)
- Bubble Tea app with session list + log viewer
- Keyboard navigation
- Live updates via polling or WebSocket client
- Package as `openclaw tui` or standalone binary

### Phase 3: Web UI (3-5 days)
- React app served by dashboard server
- Overview + Session Detail + Process Monitor
- WebSocket integration for live logs
- Package as static files bundled with the server

### Phase 4: Branch Previewer (2 days)
- Git branch management UI
- Headless screenshot capture per branch
- Side-by-side comparison view

---

## Open Questions
1. **Plugin vs standalone?** Could be an OpenClaw skill, a gateway plugin, or a separate npm package
2. **Auth model?** Localhost-only is simplest. Token auth for remote access (e.g., from phone)?
3. **Persistent storage?** Session cost history, screenshots — SQLite? Or just read from OpenClaw's existing files?
4. **Multi-gateway?** Support monitoring multiple OpenClaw instances from one dashboard?
5. **Should the web UI be the default OpenClaw interface?** Could replace/complement the messaging-first model for local use.

---

## Why Both?
- **TUI** = fast, SSH-able, works everywhere, zero dependencies, power-user workflow
- **Web** = rich visuals, screenshots, diff views, shareable, non-technical friendly
- Shared API means building one doesn't slow the other
- TUI ships fast (Phase 2), Web follows (Phase 3)
