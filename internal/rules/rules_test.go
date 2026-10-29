package rules_test

import (
	"testing"

	"utrack/internal/rules"
)

func TestDefaultCategorization(t *testing.T) {
	cfg := rules.Load()

	tests := []struct {
		appID    string
		title    string
		expected string
	}{
		{"firefox", "Rick Astley - Never Gonna Give You Up (Official Music Video) - YouTube", "YouTube"},
		{"firefox", "Lo-Fi Beats - YouTube Music", "YouTube Music"},
		{"firefox", "how to write sway ipc client in go - Google Search", "Google Search"},
		{"firefox", "tanz/utrack: Lightweight Sway time tracker - GitHub", "GitHub"},
		{"foot", "nvim cmd/utrack/main.go", "nvim cmd/utrack/main.go"},
	}

	for _, tc := range tests {
		got := cfg.CategorizeTitle(tc.appID, tc.title)
		if got != tc.expected {
			t.Errorf("CategorizeTitle(%q, %q) = %q; want %q", tc.appID, tc.title, got, tc.expected)
		}
	}
}
