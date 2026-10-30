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
