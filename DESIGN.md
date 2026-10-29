# Architecture & Technical Design: `utrack` (Sway Time Tracker)

A minimal-footprint, event-driven time tracking daemon and on-demand local web dashboard for Sway on Arch Linux.

---

## 1. Critique & Design Decisions

### 1.1 Sway IPC: Library vs. Custom Protocol Client
* **Existing Libraries:** (`joshuar/go-sway`, `mdempsky/sway`).
* **Problem:** External Go sway libraries are often unmaintained, pull bloated dependency trees, or parse the full Sway AST.
* **Decision:** Implement a **zero-dependency native Sway IPC reader** (~100–150 LOC). Sway uses the i3 IPC wire protocol:
  * 6 bytes: magic string `i3-ipc`
  * 4 bytes: `uint32` payload length (little-endian)
  * 4 bytes: `uint32` message/event type (little-endian)
  * Payload: JSON payload
* Connecting to `$SWAYSOCK` and subscribing to `["window"]` events runs with zero polling overhead.

### 1.2 Binary Architecture: Daemon vs. Web Server
* **User Idea:** Daemon collects data; separate client starts to view data and closes.
* **Refined Design:** Single binary `utrack` with subcommands:
  * `utrack daemon`: Background service. Connects to `$SWAYSOCK`, writes transitions to SQLite. Minimal RSS (~8–15 MB). Zero network sockets open.
  * `utrack web [--port 8080]`: On-demand web server. Reads the SQLite DB (WAL mode allows concurrent read/write), serves an embedded minimal dashboard, opens the browser (or prints URL), and can be killed (`Ctrl+C`) when done.
  * *Alternative option:* Daemon embeds an HTTP server on `127.0.0.1:<port>`. Cost is ~zero CPU when idle, but keeping it as a separate subcommand guarantees zero network surface until opened.

### 1.3 Database & SQLite Driver Choice
* **Driver:** In Go, standard `mattn/go-sqlite3` requires CGO (C compiler, libc linkage). Pure Go SQLite (`modernc.org/sqlite`) allows pure Go static compilation without CGO.
* **Storage Pattern:** WAL mode (`PRAGMA journal_mode=WAL;`), synchronous NORMAL.
* **Granularity:** Focus transition intervals rather than time-tick snapshots.
  * Start focus on Window A $\to$ Record timestamp.
  * Switch focus to Window B $\to$ Close Window A interval `[start, end, duration]`, begin Window B interval.

### 1.4 The "Window Title" Tracking Problem
* Browsers and media players fire `window::title` change events continuously (e.g., video timestamps, notifications, live doc edits).
* **Pitfall:** Recording an event on every title change causes database bloat and excessive writes.
* **Solution:**
  1. Only record title at initial focus transition, OR
  2. Throttle/debounce title changes within the active window (e.g., minimum 5–10s threshold before splitting an interval on title change).

---

## 2. Technical Specification

### 2.1 Process Flow

```
[ Sway Compositor ]
       |
       | UNIX domain socket ($SWAYSOCK)
       | Event: "window" (change: "focus" | "title")
       v
[ utrack daemon ]
       |
       | Batch / Immediate Commit
       v
[ SQLite Database (~/.local/share/utrack/utrack.db) ]
       ^
       | Read-only queries (WAL mode)
       |
[ utrack web ] <----> [ Browser: Vanilla HTML/CSS/Canvas ]
```

### 2.2 Database Schema

```sql
CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    app_id TEXT NOT NULL,          -- Native Wayland app_id or XWayland window class
    title TEXT NOT NULL,           -- Window title at time of tracking
    start_time INTEGER NOT NULL,   -- Unix epoch in seconds (or ms)
    end_time INTEGER NOT NULL,     -- Unix epoch in seconds (or ms)
    duration_s INTEGER NOT NULL    -- Pre-calculated duration in seconds
);

CREATE INDEX IF NOT EXISTS idx_activities_start ON activities(start_time);
CREATE INDEX IF NOT EXISTS idx_activities_app ON activities(app_id);
```

### 2.3 Dashboard UI (Frontend)
* Pure HTML5 + CSS + Vanilla JS (embedded into Go binary via `//go:embed`).
* No bundlers, no npm, no React.
* Visualization: Lightweight native HTML5 `<canvas>` horizontal timeline chart + aggregated app table.
* Range selection: "Today", "Yesterday", "Last 7 Days", custom date picker.

---

## 3. MVP Implementation Phases

1. **Phase 1: Minimal Sway IPC Client**
   - Connect to `$SWAYSOCK`.
   - Send subscribe payload `["window"]`.
   - Stream and decode `focus` and `title` events.
2. **Phase 2: Transition Tracker & SQLite Store**
   - Track active interval state in memory.
   - Flush completed interval on focus change or clean daemon shutdown (`SIGTERM`/`SIGINT`).
   - Pure Go SQLite integration with WAL mode.
3. **Phase 3: Web Dashboard & Query API**
   - HTTP endpoints: `/api/summary?from=...&to=...`, `/api/timeline?date=...`.
   - Embedded single-page dashboard.
4. **Phase 4 (Future / Follow-up):**
   - Idle detection integration (`swayidle` / lock screen).
