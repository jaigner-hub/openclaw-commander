# OpenClaw TUI

A terminal dashboard for monitoring and interacting with [OpenClaw](https://github.com/openclaw/openclaw) agents.

![Go](https://img.shields.io/badge/Go-1.24-blue) ![License](https://img.shields.io/badge/license-MIT-green)

## Features

- **Sessions** — View active agent sessions across all channels (Signal, Matrix, Discord, etc.)
- **Messaging** — Send messages directly to any session from the TUI
- **Spawn** — Create new agent sessions with custom prompts and model selection
- **Processes** — Monitor running claude/openclaw processes (reads from `~/.openclaw/process-list.json` or falls back to `ps`)
- **History** — Browse archived sub-agent transcripts after they complete
- **Live refresh** — Sessions poll every 5s, processes every 3s, logs every 2s
- **Search/filter** — Filter sessions, processes, or history with `/`
- **Follow mode** — Auto-scroll logs as new content arrives
- **Verbose levels** — Cycle through tool display modes (off/summary/full) with `v`

## Install

```bash
go install github.com/jaigner-hub/openclaw-tui@latest
```

Or build from source:

```bash
git clone git@github.com:jaigner-hub/openclaw-tui.git
cd openclaw-tui
go build -o openclaw-tui .
```

## Usage

```bash
./openclaw-tui
```

The TUI auto-discovers your gateway config from `~/.openclaw/openclaw.json`.

### Flags

```
--url     Gateway URL (default: http://127.0.0.1:18789)
--token   Gateway auth token (default: from config file)
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate list |
| `←/→` or `h/l` | Switch between list and log panels |
| `Tab` | Switch between panels |
| `Enter` | View session history / process logs / transcript |
| `m` | Message selected session |
| `s` | Spawn new agent session |
| `1` | Sessions tab |
| `2` | Processes tab |
| `3` | History tab (archived sub-agent runs) |
| `/` | Search/filter |
| `f` | Toggle follow mode (auto-scroll) |
| `v` | Cycle verbose level (off → summary → full) |
| `pgup/pgdown` or `ctrl+u/ctrl+d` | Page up/down in logs |
| `x` | Kill process (with confirmation) |
| `q` or `ctrl+c` | Quit |

### Spawn Form Keybindings

When spawning a new agent (`s`):

| Key | Action |
|-----|--------|
| `Tab` | Next field |
| `↑/↓` | Select model |
| `Enter` | Spawn agent |
| `Esc` | Cancel |

## Architecture

- **Sessions & History** — Fetched via Gateway HTTP API (`/tools/invoke`)
- **Processes** — Reads from `~/.openclaw/process-list.json` (populated by OpenClaw heartbeat), falls back to `ps` scan
- **Messaging** — Shells out to `openclaw agent --session-id <id> --message "..."`
- **Spawning** — Shells out to `openclaw agent --message "..." --session-id <id>` (runs detached in background)
- **History** — Reads orphaned `.jsonl` transcript files from `~/.openclaw/agents/main/sessions/`

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## License

MIT
