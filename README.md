# <img src="internal/web/static/favicon.svg" width="28" height="28" alt="utrack logo" valign="middle"> utrack

A lightweight, zero-polling screen time tracking daemon and local web dashboard designed for Sway on Arch Linux.

---

## Features

- **Zero-CPU Polling:** Native Sway IPC event client (`$SWAYSOCK`), waking only on window focus or title transitions.
- **Low Footprint:** Pure Go implementation (~8–15 MB RSS) using zero-CGO SQLite (`modernc.org/sqlite`).
- **Debounced Titles:** 5-second window title debounce prevents database churn from video timestamps or live doc edits.
- **Title Categorization:** Group noisy browser tabs and windows into clean categories using regex rules.
- **Media Tracking:** Listens on D-Bus MPRIS2 to track audio/video playback duration — YouTube, YouTube Music, Spotify, VLC, and more — independently of which window has focus.
- **Lock/Suspend Awareness:** Pauses tracking while the screen is locked (swayidle hooks) and discards wall-clock gaps longer than a threshold (suspend/shutdown) — only genuinely active time is counted.
- **Empty-Workspace Exclusion:** Focus on an empty workspace counts as no activity.
- **On-Demand Local UI:** Pure vanilla HTML5/CSS/Canvas dashboard with collapsible title accordions, timeline hover, and day drilldown with dark/light themes.
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

## Lock & Idle Integration (swayidle)

Sway's IPC doesn't broadcast lock/unlock events (`ext-session-lock-v1` is a protocol between the locker and the compositor, not a client-subscribable event), so lock exclusion is done by bracketing `swaylock` with pause/resume hooks. Since `swaylock` stays in the foreground until you unlock (no `-f`), `resume` runs **after** the real unlock — not on mere keyboard input.

Example `~/.config/sway/config`:

```sway
exec swayidle -w \
      timeout 300 '(utrack pause idle; swaylock -c 000000; utrack resume idle) &' \
      timeout 900 'swaymsg "output * dpms off"' \
      resume 'swaymsg "output * dpms on"' \
      before-sleep 'utrack pause suspend; playerctl stop; swaylock -c 000000; utrack resume suspend'

# Manual lock keybinding
bindsym $mod+Escape exec sh -c 'utrack pause manual; swaylock -c 000000; utrack resume manual'
```

Notes:

- **Do not use `swaylock -f` inside these hooks.** It daemonizes and returns immediately, so `resume idle` would fire before the screen is actually unlocked.
- The `timeout 300` hook is wrapped in `(...) &` so the pause group runs in the background and doesn't block swayidle; `before-sleep` stays in the foreground.
- If your `PATH` isn't inherited into swayidle's hooks (e.g. sway started from a login manager), use the absolute binary path (`/home/tanz/go/bin/utrack ...`). Check with `pgrep -af swayidle` — it shows the resolved commands.
- Long suspends are handled twice: by the `utrack pause suspend` / `resume suspend` hooks and by the daemon's own wall-clock sleep detection (any gap larger than ~90 s between heartbeats is discarded).

---

## How Time Is Accounted For

- **Active time:** flushed each 60 s heartbeat and on every focus/title transition; the 5 s title debounce keeps trailing time on the previous title.
- **Excluded:** time on an empty workspace, while locked (via the pause hooks above), and suspend/shutdown gaps — a wall-clock gap exceeding 90 s triggers sleep detection, and only time up to the last confirmed heartbeat is credited.
- **Allowed:** small boundary gaps between intervals (per-second boundary alignment); intervals never overlap.

---

## CLI Usage

| Command | Description |
|---|---|
| `utrack daemon` | Starts background tracking listener |
| `utrack web [--port 8080]` | Launches local web dashboard |
| `utrack pause [REASON]` | Pause activity tracking (default reason: `manual`) |
| `utrack resume [REASON]` | Resume activity tracking (default reason: `manual`) |
| `utrack status` | CLI summary of today's screen time and daemon state |
| `utrack clear [--force]` | Purges activity logs and reclaims disk space |

Pause/resume commands talk to the running daemon over Sway IPC tick events and exit with an error if the daemon isn't running.

### Reason semantics

Each pause registers a named reason; each resume removes one. Tracking resumes only when **all** reasons are cleared, so overlapping pauses (e.g. idle lock + suspend) can't accidentally resume tracking early. Reasons cannot contain `:` or newlines.

```bash
utrack pause lock-screen   # flushes the active interval, stops counting
utrack pause suspend       # second, independent pause
utrack resume lock-screen  # still paused (suspend reason active)
utrack resume suspend      # now fully resumed; re-detects the focused window
```

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
- **Rules:** `~/.config/utrack/rules.json`

### Example `~/.config/utrack/rules.json`:

The config file has two independent sections:

- **`rules`** — regex patterns matched against window titles to group browser tabs and windows into labelled categories.
- **`media_rules`** — matched against MPRIS player name and playback URL/title to categorize media sessions into `Music` or `Video`.

For `media_rules`, `player` is a substring match against the MPRIS well-known name (e.g. `firefox`, `spotify`, `vlc`). `pattern` is an optional regex matched against the track URL or title — useful to split a browser player (Firefox) into YouTube vs YouTube Music vs Netflix based on the URL the page reports via MPRIS metadata.

```json
{
  "rules": [
    { "pattern": "(?i)youtube music", "label": "Music" },
    { "pattern": "(?i)youtube",        "label": "YouTube" },
    { "pattern": "(?i)github",         "label": "GitHub" },
    { "pattern": "(?i)reddit",         "label": "Reddit" },
    { "pattern": "(?i)stack\\s*overflow", "label": "Stack Overflow" }
  ],
  "media_rules": [
    { "player": "spotify",  "category": "Music", "label": "Spotify" },
    { "player": "firefox",  "pattern": "(?i)music\\.youtube\\.com|youtube music", "category": "Music", "label": "YouTube Music" },
    { "player": "chromium", "pattern": "(?i)music\\.youtube\\.com|youtube music", "category": "Music", "label": "YouTube Music" },
    { "player": "firefox",  "pattern": "(?i)youtube\\.com",  "category": "Video", "label": "YouTube" },
    { "player": "chromium", "pattern": "(?i)youtube\\.com",  "category": "Video", "label": "YouTube" },
    { "player": "firefox",  "pattern": "(?i)netflix\\.com|primevideo\\.com|plex\\.tv", "category": "Video", "label": "Video Streaming" },
    { "player": "vlc",      "category": "Video", "label": "VLC Media" },
    { "player": "mpv",      "category": "Video", "label": "Local Video" }
  ]
}
```

Rules are evaluated top-to-bottom; the first match wins. If no `media_rules` key is present the daemon falls back to built-in defaults.
