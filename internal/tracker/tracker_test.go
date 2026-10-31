package tracker_test

import (
	"path/filepath"
	"testing"
	"time"

	"utrack/internal/storage"
	"utrack/internal/tracker"
)

func TestTrackerTransitions(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer db.Close()

	tr := tracker.New(db)

	baseTime := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	// Step 1: Focus Firefox
	tr.OnFocus("firefox", "GitHub - Mozilla Firefox", baseTime)

	// Step 2: 100 seconds later, focus Foot terminal
	tr.OnFocus("foot", "tmux", baseTime.Add(100*time.Second))

	// Step 3: 50 seconds later, flush on shutdown
	tr.Flush(baseTime.Add(150 * time.Second))

	// Verify database content
	summary, err := db.GetSummary(baseTime.Add(-10*time.Second).Unix(), baseTime.Add(200*time.Second).Unix())
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	if len(summary) != 2 {
		t.Fatalf("expected 2 apps recorded, got %d", len(summary))
	}

	if summary[0].AppID != "firefox" || summary[0].DurationS != 100 {
		t.Errorf("expected firefox 100s, got %+v", summary[0])
	}
	if summary[1].AppID != "foot" || summary[1].DurationS != 50 {
		t.Errorf("expected foot 50s, got %+v", summary[1])
	}

	// Step 4: Test Heartbeat & Sleep Jump Detection
	// Heartbeat after 60s should record normal interval
	tSleepStart := baseTime.Add(200 * time.Second)
	tr.OnFocus("foot", "tmux", tSleepStart)

	// Heartbeat 60s later: normal incremental flush
	tr.Heartbeat(tSleepStart.Add(60 * time.Second))

	// System sleeps for 3 hours (10800s gap)
	wakeTime := tSleepStart.Add(60*time.Second + 3*time.Hour)
	tr.Heartbeat(wakeTime) // should detect jump and clamp

	// Verify foot duration: should only have ~50s + 60s + max 60s clamp, NOT 3 hours
	summaryPostSleep, err := db.GetSummary(baseTime.Add(-10*time.Second).Unix(), wakeTime.Add(10*time.Second).Unix())
	if err != nil {
		t.Fatalf("get summary post sleep: %v", err)
	}
	for _, app := range summaryPostSleep {
		if app.AppID == "foot" && app.DurationS > 300 {
			t.Errorf("foot accumulated sleep time! duration = %d seconds", app.DurationS)
		}
	}
}

func TestHeartbeatSuspendGapUsesWallClockAndDoesNotSpanSleep(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer db.Close()

	tr := tracker.New(db)
	base := time.Now()
	tr.OnFocus("firefox", "Wave File Specifications", base)

	// Normal heartbeat, then a wake 3 hours later. This models the wall-clock
	// gap observed after suspend; sleep must not appear as one long activity,
	// nor should the unconfirmed time after the last heartbeat be estimated.
	tr.Heartbeat(base.Add(tracker.HeartbeatInterval))
	wake := base.Add(tracker.HeartbeatInterval + 3*time.Hour)
	tr.Heartbeat(wake)

	rows, err := db.GetTimeline(base.Unix()-1, wake.Unix()+1, 100)
	if err != nil {
		t.Fatalf("get timeline: %v", err)
	}
	for _, row := range rows {
		if row.EndTime-row.StartTime > int64(tracker.HeartbeatInterval.Seconds()) {
			t.Errorf("activity spans suspend: %+v (wall interval %ds)", row, row.EndTime-row.StartTime)
		}
		if row.DurationS > int64(tracker.HeartbeatInterval.Seconds()) {
			t.Errorf("credited more than one heartbeat across suspend: %+v", row)
		}
	}
	if len(rows) == 0 {
		t.Fatal("expected pre-suspend activity to be flushed")
	}
	if len(rows) != 1 || rows[0].EndTime > base.Add(tracker.HeartbeatInterval).Unix() {
		t.Errorf("unconfirmed time after last heartbeat was credited: %+v", rows)
	}

	appID, title, started := tr.GetCurrentState()
	if appID != "firefox" || title != "Wave File Specifications" || !started.Equal(wake) {
		t.Errorf("post-wake state = (%q, %q, %v), want firefox/title starting at wake %v", appID, title, started, wake)
	}
}

func TestFocusEventAfterSuspendCannotFlushSleepDuration(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer db.Close()

	tr := tracker.New(db)
	base := time.Now()
	tr.OnFocus("firefox", "Wave File Specifications", base)

	// Model Sway delivering focus before the post-wake heartbeat, including
	// the same window still being focused when the laptop wakes.
	wake := base.Add(10 * time.Hour)
	tr.OnFocus("firefox", "Wave File Specifications", wake)
	tr.Flush(wake.Add(10 * time.Second))

	rows, err := db.GetTimeline(base.Unix()-1, wake.Unix()+20, 100)
	if err != nil {
		t.Fatalf("get timeline: %v", err)
	}
	for _, row := range rows {
		if row.EndTime-row.StartTime > int64(tracker.HeartbeatInterval.Seconds()) {
			t.Errorf("focus event flushed sleep interval: %+v", row)
		}
	}
	if len(rows) == 0 {
		t.Fatal("expected bounded pre-suspend or post-wake activity")
	}
	if got := rows[len(rows)-1].StartTime; got < wake.Unix() {
		t.Errorf("post-wake interval began before wake: %d < %d", got, wake.Unix())
	}
}

// A pending (debounced) title must not delay or lose time when focus leaves:
// the whole interval up to the switch gets flushed under the previous title.
func TestPendingTitleCanceledOnFocusSwitch(t *testing.T) {
	tempDir := t.TempDir()
	db, err := storage.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer db.Close()

	tr := tracker.New(db)
	base := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

	// Foot focused at T, title change debounced at T+3s (5s timer pending).
	tr.OnFocus("foot", "zsh", base)
	tr.OnTitleChange("foot", "nvim main.go", base.Add(3*time.Second))

	// Switch to an empty workspace at T+6s, before the 5s debounce fires at T+8s.
	tr.OnFocus("", "", base.Add(6*time.Second))

	titles, err := db.GetAppTitles("foot", base.Unix()-10, base.Add(20*time.Second).Unix())
	if err != nil {
		t.Fatalf("get titles: %v", err)
	}
	total := int64(0)
	for _, ti := range titles {
		if ti.Title == "nvim main.go" && ti.DurationS > 0 {
			t.Errorf("cancelled pending title wrongly recorded: %+v", ti)
		}
		total += ti.DurationS
	}
	if total != 6 {
		t.Errorf("expected full 6s under previous title, got %d (%+v)", total, titles)
	}
}
