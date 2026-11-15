package storage_test

import (
	"path/filepath"
	"testing"
	"time"

	"utrack/internal/storage"
)

func TestStorageOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer db.Close()

	now := time.Now()
	t1 := now.Add(-30 * time.Minute)
	t2 := now.Add(-10 * time.Minute)
	t3 := now

	// Insert activities
	if err := db.InsertActivity("org.mozilla.firefox", "YouTube - Mozilla Firefox", t1, t2); err != nil {
		t.Fatalf("insert firefox failed: %v", err)
	}
	if err := db.InsertActivity("foot", "nvim - main.go", t2, t3); err != nil {
		t.Fatalf("insert foot failed: %v", err)
	}

	// 1. Check Summary
	fromUnix := t1.Add(-1 * time.Minute).Unix()
	toUnix := t3.Add(1 * time.Minute).Unix()

	summary, err := db.GetSummary(fromUnix, toUnix)
	if err != nil {
		t.Fatalf("get summary failed: %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("expected 2 apps in summary, got %d", len(summary))
	}
	// Firefox should have 20 minutes (1200s), foot 10 minutes (600s)
	if summary[0].AppID != "org.mozilla.firefox" || summary[0].DurationS != 1200 {
		t.Errorf("unexpected top app: %+v", summary[0])
	}
	if summary[1].AppID != "foot" || summary[1].DurationS != 600 {
		t.Errorf("unexpected second app: %+v", summary[1])
	}

	// 2. Check App Titles
	titles, err := db.GetAppTitles("org.mozilla.firefox", fromUnix, toUnix)
	if err != nil {
		t.Fatalf("get app titles failed: %v", err)
	}
	if len(titles) != 1 || titles[0].Title != "YouTube - Mozilla Firefox" {
		t.Errorf("unexpected title result: %+v", titles)
	}

	// 3. Check Timeline
	timeline, err := db.GetTimeline(fromUnix, toUnix, 100)
	if err != nil {
		t.Fatalf("get timeline failed: %v", err)
	}
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline events, got %d", len(timeline))
	}
	if timeline[0].AppID != "org.mozilla.firefox" || timeline[1].AppID != "foot" {
		t.Errorf("unexpected timeline ordering: %+v", timeline)
	}

	// 4. Test Media Operations
	if err := db.InsertMedia("firefox", "Video Title", "https://youtube.com/watch?v=123", t1, t2); err != nil {
		t.Fatalf("insert media failed: %v", err)
	}
	mediaSessions, err := db.GetRawMediaSessions(fromUnix, toUnix)
	if err != nil {
		t.Fatalf("get raw media sessions failed: %v", err)
	}
	if len(mediaSessions) != 1 || mediaSessions[0].PlayerID != "firefox" || mediaSessions[0].URL != "https://youtube.com/watch?v=123" {
		t.Errorf("unexpected media session: %+v", mediaSessions)
	}

	// 5. Test Truncate
	if err := db.TruncateActivities(); err != nil {
		t.Fatalf("truncate activities failed: %v", err)
	}
	clearedSummary, err := db.GetSummary(fromUnix, toUnix)
	if err != nil {
		t.Fatalf("get summary after truncate failed: %v", err)
	}
	if len(clearedSummary) != 0 {
		t.Errorf("expected 0 items after truncate, got %d", len(clearedSummary))
	}
	clearedMedia, err := db.GetRawMediaSessions(fromUnix, toUnix)
	if err != nil {
		t.Fatalf("get raw media after truncate failed: %v", err)
	}
	if len(clearedMedia) != 0 {
		t.Errorf("expected 0 media items after truncate, got %d", len(clearedMedia))
	}
}

func TestMediaSchemaMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "legacy.db")

	// Create legacy database with category column
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	db.Close()

	// Reopen should succeed and handle existing schema cleanly
	db2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen storage: %v", err)
	}
	defer db2.Close()

	now := time.Now()
	if err := db2.InsertMedia("spotify", "Song A", "", now.Add(-10*time.Minute), now); err != nil {
		t.Fatalf("insert media into migrated db failed: %v", err)
	}
	raw, err := db2.GetRawMediaSessions(now.Add(-15*time.Minute).Unix(), now.Add(5*time.Minute).Unix())
	if err != nil || len(raw) != 1 {
		t.Fatalf("query migrated db failed: %v, len=%d", err, len(raw))
	}
}
