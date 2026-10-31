package rules

import (
	"encoding/json"
	"log"
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
// Invalid config content never overwrites the file: the error is logged and
// built-in defaults are used instead.
func Load() *Config {
	cfgPath := GetConfigPath()
	var cfg Config

	data, err := os.ReadFile(cfgPath)
	switch {
	case err == nil:
		if jErr := json.Unmarshal(data, &cfg); jErr != nil || len(cfg.Rules) == 0 {
			log.Printf("[rules] %s is empty or invalid (%v); using built-in defaults, file untouched", cfgPath, jErr)
		} else {
			compileRules(&cfg)
			return &cfg
		}
	case os.IsNotExist(err):
		// First run: write template config if directory is writable
	default:
		log.Printf("[rules] cannot read %s (%v); using built-in defaults, file untouched", cfgPath, err)
	}

	// Fallback to default rules
	cfg.Rules = append([]Rule{}, DefaultRules...)
	compileRules(&cfg)

	if os.IsNotExist(err) && filepath.Dir(cfgPath) != "." {
		if mkErr := os.MkdirAll(filepath.Dir(cfgPath), 0755); mkErr == nil {
			if out, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
				_ = os.WriteFile(cfgPath, out, 0644)
			}
		}
	}

	return &cfg
}

func compileRules(cfg *Config) {
	kept := cfg.Rules[:0]
	for _, r := range cfg.Rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			log.Printf("[rules] dropping rule %q (%s): invalid pattern %q: %v", r.Label, cfgPathLabel(r), r.Pattern, err)
			continue
		}
		r.regex = re
		kept = append(kept, r)
	}
	cfg.Rules = kept
}

// cfgPathLabel describes where a rule applies (app_id or global).
func cfgPathLabel(r Rule) string {
	if r.AppID != "" {
		return "app " + r.AppID
	}
	return "global"
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
