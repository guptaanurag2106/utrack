package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"utrack/internal/storage"
	"utrack/internal/web"
)

func TestMediaEndpointsDynamicCategorization(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer db.Close()

	now := time.Now()
	t1 := now.Add(-20 * time.Minute)
	t2 := now.Add(-10 * time.Minute)
	t3 := now

	// Insert raw media sessions
	if err := db.InsertMedia("firefox", "Never Gonna Give You Up", "https://youtube.com/watch?v=123", t1, t2); err != nil {
		t.Fatalf("insert youtube media: %v", err)
	}
	if err := db.InsertMedia("spotify", "Song Name", "", t2, t3); err != nil {
		t.Fatalf("insert spotify media: %v", err)
	}

	server := web.NewServer(db, 0)
	handler := server.Handler()

	// 1. Test /api/media
	req := httptest.NewRequest("GET", "/api/media?from=0&to=9999999999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/api/media returned status %d", rec.Code)
	}

	var mediaSummary []storage.MediaSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &mediaSummary); err != nil {
		t.Fatalf("unmarshal media summary: %v", err)
	}
	if len(mediaSummary) != 2 {
		t.Fatalf("expected 2 media summary items, got %d", len(mediaSummary))
	}

	// Spotify should be categorized as Music by default media rules
	foundSpotify := false
	foundYouTube := false
	for _, m := range mediaSummary {
		if m.PlayerID == "spotify" {
			foundSpotify = true
			if m.Category != "Music" {
				t.Errorf("expected Spotify category 'Music', got %q", m.Category)
			}
		}
		if m.PlayerID == "firefox" {
			foundYouTube = true
			if m.Category != "Video" && m.Category != "Media" {
				t.Errorf("unexpected category for youtube: %q", m.Category)
			}
		}
	}
	if !foundSpotify || !foundYouTube {
		t.Errorf("missing expected media items: foundSpotify=%v, foundYouTube=%v", foundSpotify, foundYouTube)
	}

	// 2. Test /api/media-timeline
	reqTl := httptest.NewRequest("GET", "/api/media-timeline?from=0&to=9999999999", nil)
	recTl := httptest.NewRecorder()
	handler.ServeHTTP(recTl, reqTl)

	if recTl.Code != http.StatusOK {
		t.Fatalf("/api/media-timeline returned status %d", recTl.Code)
	}

	var timeline []storage.MediaRecord
	if err := json.Unmarshal(recTl.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("unmarshal media timeline: %v", err)
	}
	if len(timeline) != 2 {
		t.Fatalf("expected 2 timeline items, got %d", len(timeline))
	}
}
