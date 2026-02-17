# OpenClaw Commander

A TUI dashboard for monitoring and interacting with [OpenClaw](https://github.com/openclaw/openclaw) agents.

![Go](https://img.shields.io/badge/Go-1.24-blue) ![License](https://img.shields.io/badge/license-MIT-green)

## Features

- **Sessions** — View active agent sessions across all channels (Signal, Matrix, Discord, etc.)
- **Messaging** — Send messages directly to any session from the TUI
- **Processes** — Monitor running claude/openclaw processes
- **History** — Browse archived sub-agent transcripts after they complete
- **Live refresh** — Sessions poll every 5s, processes every 3s, logs every 2s
- **Search/filter** — Filter sessions, processes, or history with `/`
- **Follow mode** — Auto-scroll logs as new content arrives

## Install

```bash
go install github.com/jaigner-hub/openclaw-commander@latest
```

Or build from source:

```bash
git clone git@github.com:jaigner-hub/openclaw-commander.git
cd openclaw-commander
go build -o oclaw-tui .
```

## Usage

```bash
./oclaw-tui
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
| `Enter` | View session history / process logs / transcript |
| `m` | Message selected session |
| `Tab` | Switch between list and log panels |
| `1` | Sessions tab |
| `2` | Processes tab |
| `3` | History tab (archived sub-agent runs) |
| `/` | Search/filter |
| `f` | Toggle follow mode (auto-scroll) |
| `x` | Kill process |
| `q` | Quit |

## Architecture

- **Sessions & History** — Fetched via Gateway HTTP API (`/tools/invoke`)
- **Processes** — Scanned from OS via `ps` (the process tool isn't exposed on the gateway HTTP API)
- **Messaging** — Shells out to `openclaw agent --session-id <id> --message "..."`
- **History** — Reads orphaned `.jsonl` transcript files from `~/.openclaw/agents/main/sessions/`

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## License

MIT
