# AGENTS.md

## Project Overview
`utrack` is a lightweight, low-overhead time tracking daemon and local web dashboard designed for Sway on Arch Linux.
It tracks active focused applications and window titles with zero CPU polling via Sway's native IPC UNIX socket, and tracks background audio/video playback sessions via standard D-Bus MPRIS2 interfaces.

## Key Principles & Design Constraints
1. **Performance First:**
   - Event-driven via `$SWAYSOCK` rather than polling tree dumps.
   - D-Bus signal subscription for MPRIS2 (`PropertiesChanged` on `/org/mpris/MediaPlayer2`).
   - Low memory footprint (~8-15 MB RSS) and zero idle CPU cycles.
   - Title event debouncing (~5 seconds) to prevent database churn from rapid title mutations (e.g. video elapsed times).
2. **Raw Storage & Dynamic Categorization (Unified Architecture):**
   - SQLite stores raw events only:
     - `activities`: raw `app_id` and window `title`.
     - `media_sessions`: raw `player_id`, track `title`, and playback `url`.
   - The daemon does zero categorization logic and does not depend on rules; rule edits never require daemon restarts.
   - `utrack web` dynamically compiles and evaluates regex rules (`rules` and `media_rules` from `~/.config/utrack/rules.json`) at query time in memory (< 1 ms latency).
   - Rules automatically hot-reload in `utrack web` when `rules.json` modification time updates.
3. **Minimal Dependencies:**
   - Standard Go library wherever possible.
   - Custom Sway IPC client (14-byte binary framing `i3-ipc` + JSON payloads).
   - D-Bus MPRIS client via pure Go `github.com/godbus/dbus/v5`.
   - Pure Go SQLite driver (`modernc.org/sqlite`) for zero-CGO compilation.
   - Pure vanilla HTML5/CSS/Canvas frontend embedded into the binary with `//go:embed`.
4. **Robustness & Reliability:**
   - Single-instance enforcement using POSIX file locks (`flock`).
   - Clean interval flushing on shutdown (`SIGINT`, `SIGTERM`).
   - Auto-reconnection on Sway socket disconnect / restart.
   - Pause / resume commands with named reasons for overlapping lock screen and suspend hooks.
   - System desktop notifications (`notify-send`) on unrecoverable daemon errors.
5. **Binary Layout:**
   - Unified single CLI:
     - `utrack daemon`: Runs the background listener and records intervals into SQLite.
     - `utrack web [--port 8080] [--no-open]`: On-demand local web server with embedded UI. Auto-opens default browser. Exits on `Ctrl+C`.
     - `utrack pause [REASON]`: Pauses activity tracking under a named reason.
     - `utrack resume [REASON]`: Resumes activity tracking when all reasons clear.
     - `utrack status`: Quick CLI status check (daemon running/stopped, current active window).
     - `utrack clear [--force]`: Clears all tracked activities from SQLite.

## File Structure
- `cmd/utrack/main.go`: CLI entrypoint (subcommands: `daemon`, `web`, `pause`, `resume`, `status`, `clear`).
- `internal/ipc/`: Zero-dependency Sway IPC client (event subscription, tree query, message framing).
- `internal/mpris/`: MPRIS2 D-Bus listener recording audio/video playback intervals into SQLite.
- `internal/rules/`: Window title and MPRIS media regex categorization (`~/.config/utrack/rules.json`).
- `internal/storage/`: SQLite connection, migrations, WAL mode setup, interval write operations, query aggregations.
- `internal/tracker/`: State machine handling window focus transitions, title debouncing, and interval closing.
- `internal/web/`: Embedded HTTP server, REST endpoints, and vanilla JS/CSS frontend with collapsible title accordions.
- `internal/lock/`: Process single-instance lock via `flock`.

## Environment & Paths
- Sway socket: `$SWAYSOCK` (fallback to `I3SOCK`).
- Database: `$XDG_DATA_HOME/utrack/utrack.db` (fallback: `~/.local/share/utrack/utrack.db`).
- Lock file: `$XDG_RUNTIME_DIR/utrack.lock` (fallback: `~/.local/share/utrack/utrack.lock`).
- Rules config: `$XDG_CONFIG_HOME/utrack/rules.json` (fallback: `~/.config/utrack/rules.json`).

## Development Commands
```bash
go build -o utrack ./cmd/utrack
./utrack daemon
./utrack web
./utrack status
```
