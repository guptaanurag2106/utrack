package main

import (
	"path/filepath"
	"testing"
	"time"

	"utrack/internal/ipc"
	"utrack/internal/storage"
	"utrack/internal/tracker"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func strPtr(s string) *string { return &s }

func summaryOf(t *testing.T, db *storage.DB, from, to time.Time) map[string]int64 {
	t.Helper()
	apps, err := db.GetSummary(from.Unix()-10, to.Unix()+10)
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	out := make(map[string]int64, len(apps))
	for _, a := range apps {
		out[a.AppID] = a.DurationS
	}
	return out
}

// Sway emits workspace focus before window focus (observed: same millisecond).
// The workspace payload may or may not carry the focused flag on the child.
func windowedWorkspace(name string, withFocusedChild bool) *ipc.Node {
	ws := &ipc.Node{Name: name}
	if withFocusedChild {
		ws.Nodes = []*ipc.Node{{
			Name:    "GitHub - Mozilla Firefox",
			AppID:   strPtr("firefox"),
			Focused: true,
		}}
	}
	return ws
}

// Covers: pause on empty workspace, zero accrual while paused, and resume
// starting exactly at the resume event so no time leaks across either edge.
// Heartbeats fire on their real 60s cadence throughout.
func TestEmptyWorkspacePauseEdges(t *testing.T) {
	db := testDB(t)
	tr := tracker.New(db)

	// base is wall-clock-relative so the tracker's initial lastHeartbeat (set in
	// New) doesn't manufacture a fake sleep jump on the first heartbeat.
	base := time.Now()
	emptyWS := &ipc.WorkspaceEvent{Change: "focus", Current: &ipc.Node{Name: "4"}}
	focusFF := &ipc.WindowEvent{
		Change:    "focus",
		Container: ipc.Node{Name: "GitHub - Mozilla Firefox", AppID: strPtr("firefox")},
	}

	// 1. Focus firefox at T; heartbeats flush normally.
	dispatchWindowEvent(tr, nil, focusFF, base)
	tr.Heartbeat(base.Add(60 * time.Second)) // firefox 0..60s

	// 2. Switch to empty workspace at T+90s -> flushes the last 30s, pauses.
	dispatchWorkspaceEvent(tr, nil, emptyWS, base.Add(90*time.Second))

	// 3. Heartbeats while paused must accrue nothing (no rows between 90s and 240s).
	tr.Heartbeat(base.Add(120 * time.Second))
	tr.Heartbeat(base.Add(180 * time.Second))
	tr.Heartbeat(base.Add(240 * time.Second))

	// 4. Back to a windowed workspace at T+240s: sway sends workspace
	//    focus first and window focus right after (same ms).
	ws := &ipc.WorkspaceEvent{Change: "focus", Current: windowedWorkspace("1", true)}
	dispatchWorkspaceEvent(tr, nil, ws, base.Add(240*time.Second))
	dispatchWindowEvent(tr, nil, focusFF, base.Add(240*time.Second))

	// 5. Heartbeat and final flush on exit.
	tr.Heartbeat(base.Add(300 * time.Second)) // firefox 240..300s
	tr.Flush(base.Add(310 * time.Second))     // firefox 300..310s

	sum := summaryOf(t, db, base, base.Add(320*time.Second))
	if sum["firefox"] != 160 { // 90s pre-pause + 70s post-resume
		t.Errorf("firefox duration = %d, want 160", sum["firefox"])
	}
	if len(sum) != 1 {
		t.Errorf("unexpected phantom apps recorded: %+v", sum)
	}
	rows, err := db.GetTimeline(base.Unix()-1, base.Add(320*time.Second).Unix(), 100)
	if err != nil {
		t.Fatalf("get timeline: %v", err)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].StartTime < rows[i-1].EndTime {
			t.Errorf("overlapping intervals: previous=%+v next=%+v", rows[i-1], rows[i])
		}
	}

	// Paused stretch produced no rows with zero or bogus values.
	if sum["4"] != 0 || sum["unknown"] != 0 {
		t.Errorf("empty workspace leaked into activities: %+v", sum)
	}
}

// The workspace payload may arrive without the focused flag on its children
// (stale snapshot). Pause then resume within the same millisecond must lose
// nothing and must not double-flush overlapping time.
func TestStaleWorkspacePayloadStillResumes(t *testing.T) {
	db := testDB(t)
	tr := tracker.New(db)

	base := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

	dispatchWindowEvent(tr, nil, &ipc.WindowEvent{
		Change:    "focus",
		Container: ipc.Node{Name: "GitHub - Mozilla Firefox", AppID: strPtr("firefox")},
	}, base)
	dispatchWorkspaceEvent(tr, nil, emptyWorkspaceEvent(), base.Add(30*time.Second))

	// Stale workspace payload: no focused child visible. Pause then resume.
	stale := &ipc.WorkspaceEvent{Change: "focus", Current: windowedWorkspace("1", false)}
	dispatchWorkspaceEvent(tr, nil, stale, base.Add(60*time.Second))
	dispatchWindowEvent(tr, nil, &ipc.WindowEvent{
		Change:    "focus",
		Container: ipc.Node{Name: "GitHub - Mozilla Firefox", AppID: strPtr("firefox")},
	}, base.Add(60*time.Second))

	tr.Flush(base.Add(90 * time.Second))

	sum := summaryOf(t, db, base, base.Add(100*time.Second))
	if sum["firefox"] != 60 { // 30s pre + 30s post, pause excluded
		t.Errorf("firefox duration = %d, want 60", sum["firefox"])
	}
	if sum["unknown"] != 0 {
		t.Errorf("stale payload credited phantom app: %+v", sum)
	}
}

func emptyWorkspaceEvent() *ipc.WorkspaceEvent {
	return &ipc.WorkspaceEvent{Change: "focus", Current: &ipc.Node{Name: "4"}}
}

// A window focus event without app identity must never become a ghost app row.
func TestWindowEventWithoutIdentityPauses(t *testing.T) {
	db := testDB(t)
	tr := tracker.New(db)

	base := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	dispatchWindowEvent(tr, nil, &ipc.WindowEvent{
		Change:    "focus",
		Container: ipc.Node{Name: "GitHub - Mozilla Firefox", AppID: strPtr("firefox")},
	}, base)
	// Hypothetical sway emitting the workspace container as a window event.
	dispatchWindowEvent(tr, nil, &ipc.WindowEvent{
		Change:    "focus",
		Container: ipc.Node{Name: "4"},
	}, base.Add(15*time.Second))

	tr.Flush(base.Add(20 * time.Second))

	sum := summaryOf(t, db, base, base.Add(30*time.Second))
	if sum["firefox"] != 15 {
		t.Errorf("firefox duration = %d, want 15", sum["firefox"])
	}
	if sum["4"] != 0 {
		t.Errorf("workspace container recorded as app: %+v", sum)
	}
}
