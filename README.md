# <img src="internal/web/static/favicon.svg" width="28" height="28" alt="utrack logo" valign="middle"> utrack

A lightweight, zero-polling screen time tracking daemon and local web dashboard designed for Sway on Arch Linux.

---

## Features

- **Zero-CPU Polling:** Native Sway IPC event client (`$SWAYSOCK`), waking only on window focus or title transitions.
- **Low Footprint:** Pure Go implementation (~8–15 MB RSS) using zero-CGO SQLite (`modernc.org/sqlite`).
- **Debounced Titles:** 5-second window title debounce prevents database churn from video timestamps or live doc edits.
- **Title Categorization:** Group noisy browser tabs and windows into clean categories using regex rules.
- **On-Demand Local UI:** Pure vanilla HTML5/CSS/Canvas dashboard with collapsible title accordions and dark/light themes.
- **Single-Instance Safety:** POSIX `flock` prevents multiple concurrent daemons.

---

## Build & Install

```bash
# Option 1: Standard Go install (installs to ~/go/bin/utrack)
go install ./cmd/utrack

# Option 2: Local binary build
go build -o ~/.local/bin/utrack ./cmd/utrack
```

Ensure `~/go/bin` or `~/.local/bin` is in your `$PATH`.

---

## Running with Sway

Add to your `~/.config/sway/config`:

```sway
exec utrack daemon
```

---

## CLI Usage

| Command | Description |
|---|---|
| `utrack daemon` | Starts background tracking listener |
| `utrack web [--port 8080]` | Launches local web dashboard |
| `utrack status` | CLI summary of today's screen time and daemon state |
| `utrack clear [--force]` | Purges activity logs and reclaims disk space |

---

## Systemd User Service (Optional)

If you prefer managing `utrack` via `systemd`:

1. Copy service unit:
   ```bash
   mkdir -p ~/.config/systemd/user
   cp utrack.service ~/.config/systemd/user/
   ```
2. Enable and start:
   ```bash
   systemctl --user daemon-reload
   systemctl --user enable --now utrack.service
   ```

*(Requires `exec systemctl --user import-environment SWAYSOCK WAYLAND_DISPLAY` in your Sway config).*

---

## Paths & Configuration

- **Database:** `$XDG_DATA_HOME/utrack/utrack.db` (fallback: `~/.local/share/utrack/utrack.db`).
- **Lock File:** `$XDG_RUNTIME_DIR/utrack.lock` (fallback: `~/.local/share/utrack/utrack.lock`).
- **Title Rules:** `~/.config/utrack/rules.json`

### Example `~/.config/utrack/rules.json`:
```json
{
  "rules": [
    { "pattern": "(?i)youtube music", "label": "YouTube Music" },
    { "pattern": "(?i)youtube", "label": "YouTube" },
    { "pattern": "(?i)google( search)?", "label": "Google Search" },
    { "pattern": "(?i)github", "label": "GitHub" },
    { "pattern": "(?i)reddit", "label": "Reddit" },
    { "pattern": "(?i)stack\\s*overflow", "label": "Stack Overflow" }
  ]
}
```
