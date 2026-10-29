# AGENTS.md

## Project Overview
`utrack` is a lightweight, low-overhead time tracking daemon and local web dashboard designed for Sway on Arch Linux.
It tracks active focused applications and window titles with zero CPU polling via Sway's native IPC UNIX socket.

## Key Principles & Design Constraints
1. **Performance First:**
   - Event-driven via `$SWAYSOCK` rather than polling tree dumps.
   - Low memory footprint (~8-15 MB RSS) and zero idle CPU cycles.
   - Title event debouncing (~5 seconds) to prevent database churn from rapid title mutations (e.g. video elapsed times).
2. **Minimal Dependencies:**
   - Standard Go library wherever possible.
   - Custom Sway IPC client (14-byte binary framing `i3-ipc` + JSON payloads).
   - Pure Go SQLite driver (`modernc.org/sqlite`) for zero-CGO compilation.
   - Pure vanilla HTML5/CSS/Canvas frontend embedded into the binary with `//go:embed`.
3. **Robustness & Reliability:**
   - Single-instance enforcement using POSIX file locks (`flock`).
   - Clean interval flushing on shutdown (`SIGINT`, `SIGTERM`).
   - Auto-reconnection on Sway socket disconnect / restart.
   - System desktop notifications (`notify-send`) on unrecoverable daemon errors.
4. **Binary Layout:**
   - Unified single CLI:
     - `utrack daemon`: Runs the background listener and records intervals into SQLite.
     - `utrack web [--port 8080]`: On-demand local web server with embedded UI. Exits on `Ctrl+C`.
     - `utrack status`: Quick CLI status check (daemon running/stopped, current active window).

## File Structure
- `cmd/utrack/main.go`: CLI entrypoint (subcommands: `daemon`, `web`, `status`).
- `internal/ipc/`: Zero-dependency Sway IPC client (event subscription, tree query, message framing).
- `internal/storage/`: SQLite connection, migrations, WAL mode setup, interval write operations, query aggregations.
- `internal/tracker/`: State machine handling window focus transitions, title debouncing, and interval closing.
- `internal/web/`: Embedded HTTP server, REST endpoints, and vanilla JS/CSS frontend.
- `internal/lock/`: Process single-instance lock via `flock`.

## Environment & Paths
- Sway socket: `$SWAYSOCK` (fallback to `I3SOCK`).
- Database: `$XDG_DATA_HOME/utrack/utrack.db` (fallback: `~/.local/share/utrack/utrack.db`).
- Lock file: `$XDG_RUNTIME_DIR/utrack.lock` (fallback: `~/.local/share/utrack/utrack.lock`).

## Development Commands
```bash
go build -o utrack ./cmd/utrack
./utrack daemon
./utrack web
./utrack status
```
