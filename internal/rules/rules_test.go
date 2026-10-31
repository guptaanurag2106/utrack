package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"utrack/internal/rules"
)

func TestDefaultCategorization(t *testing.T) {
	// Set XDG_CONFIG_HOME to nonexistent dir to test defaults isolation
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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

func TestInvalidJSONKeepsFileAndUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "utrack", "rules.json")
	original := "{ not valid json"
	os.MkdirAll(filepath.Dir(cfgPath), 0755)
	os.WriteFile(cfgPath, []byte(original), 0644)

	cfg := rules.Load()

	if cfg.CategorizeTitle("firefox", "Videos - YouTube") != "YouTube" {
		t.Errorf("expected built-in defaults on invalid JSON")
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Errorf("rules.json was overwritten:\n got %q\nwant %q", data, original)
	}
}

func TestBadPatternDroppedOthersKept(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	const rc = `{"rules":[
		{"pattern":"[unbalanced", "label":"Broken"},
		{"pattern":"(?i)youtube music", "label":"YouTube Music"},
		{"pattern":"(?i)github", "label":"GitHub"}
	]}`
	os.MkdirAll(filepath.Join(dir, "utrack"), 0755)
	os.WriteFile(filepath.Join(dir, "utrack", "rules.json"), []byte(rc), 0644)

	cfg := rules.Load()

	if got := cfg.CategorizeTitle("firefox", "Mix - YouTube Music"); got != "YouTube Music" {
		t.Errorf("valid rule dropped: got %q", got)
	}
	if got := cfg.CategorizeTitle("firefox", "My Dashboard - GitHub"); got != "GitHub" {
		t.Errorf("valid rule dropped: got %q", got)
	}
	if got := cfg.CategorizeTitle("firefox", "whatever title"); got != "whatever title" {
		t.Errorf("broken rule should not match anything: got %q", got)
	}
}
