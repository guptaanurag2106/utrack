package rules_test

import (
	"testing"

	"utrack/internal/rules"
)

func TestCategorizeMedia(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := rules.Load()

	tests := []struct {
		player   string
		title    string
		url      string
		wantCat  string
		wantLbl  string
	}{
		{"spotify", "Pink Floyd - Time", "", "Music", "Spotify"},
		{"firefox", "Lofi Hip Hop - YouTube Music", "https://music.youtube.com/watch?v=123", "Music", "YouTube Music"},
		{"firefox", "Song Title - Artist", "https://music.youtube.com/watch?v=abc", "Music", "YouTube Music"},
		{"firefox", "How to Rice Sway - YouTube", "https://youtube.com/watch?v=abc", "Video", "YouTube"},
		{"firefox", "Random Video Title", "https://www.youtube.com/watch?v=xyz", "Video", "YouTube"},
		{"firefox", "Stranger Things - Netflix", "https://netflix.com/watch/123", "Video", "Video Streaming"},
		{"mpv", "Silicon Valley S01E01.mkv", "", "Video", "Local Video"},
		{"vlc", "concert.mp4", "", "Video", "VLC Media"},
		{"unknownplayer", "random song", "", "Media", "random song"},
	}

	for _, tc := range tests {
		gotCat, gotLbl := cfg.CategorizeMedia(tc.player, tc.title, tc.url)
		if gotCat != tc.wantCat || gotLbl != tc.wantLbl {
			t.Errorf("CategorizeMedia(%q, %q, %q) = (%q, %q); want (%q, %q)",
				tc.player, tc.title, tc.url, gotCat, gotLbl, tc.wantCat, tc.wantLbl)
		}
	}
}
