package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Rule defines a matching rule to map noisy window titles to a clean category.
type Rule struct {
	AppID   string `json:"app_id,omitempty"` // Optional: match only specific app_id
	Pattern string `json:"pattern"`          // Regex pattern
	Label   string `json:"label"`            // Clean grouped label
	regex   *regexp.Regexp
}

// Config holds user-defined title grouping rules.
type Config struct {
	Rules []Rule `json:"rules"`
}

// DefaultRules provides sensible defaults for common web & desktop activities.
var DefaultRules = []Rule{
	{Pattern: `(?i)youtube music`, Label: "YouTube Music"},
	{Pattern: `(?i)youtube`, Label: "YouTube"},
	{Pattern: `(?i)google search`, Label: "Google Search"},
	{Pattern: `(?i)github`, Label: "GitHub"},
	{Pattern: `(?i)reddit`, Label: "Reddit"},
	{Pattern: `(?i)stackoverflow|stack overflow`, Label: "Stack Overflow"},
	{Pattern: `(?i)twitch`, Label: "Twitch"},
	{Pattern: `(?i)discord`, Label: "Discord"},
	{Pattern: `(?i)chatgpt`, Label: "ChatGPT"},
}

// GetConfigPath returns the standard XDG path for rules.json.
func GetConfigPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configHome = filepath.Join(home, ".config")
		}
	}
	if configHome == "" {
		return "rules.json"
	}
	return filepath.Join(configHome, "utrack", "rules.json")
}

// Load compiles rules from config file, or falls back to DefaultRules.
// If config file doesn't exist, it creates one with default rules for user convenience.
func Load() *Config {
	cfgPath := GetConfigPath()
	var cfg Config

	data, err := os.ReadFile(cfgPath)
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err == nil && len(cfg.Rules) > 0 {
			compileRules(&cfg)
			return &cfg
		}
	}

	// Fallback to default rules
	cfg.Rules = append([]Rule{}, DefaultRules...)
	compileRules(&cfg)

	// Attempt to write template config if directory is writable
	if dir := filepath.Dir(cfgPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err == nil {
			if out, err := json.MarshalIndent(cfg, "", "  "); err == nil {
				_ = os.WriteFile(cfgPath, out, 0644)
			}
		}
	}

	return &cfg
}

func compileRules(cfg *Config) {
	for i := range cfg.Rules {
		r, err := regexp.Compile(cfg.Rules[i].Pattern)
		if err == nil {
			cfg.Rules[i].regex = r
		}
	}
}

// CategorizeTitle returns the rule label if pattern matches, or original title.
func (c *Config) CategorizeTitle(appID, title string) string {
	if c == nil {
		return title
	}
	for _, r := range c.Rules {
		if r.regex == nil {
			continue
		}
		if r.AppID != "" && r.AppID != appID {
			continue
		}
		if r.regex.MatchString(title) {
			return r.Label
		}
	}
	return title
}
