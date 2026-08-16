package pagewatch

import (
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	WatchModeAdditions = "additions"
	WatchModeChanges   = "changes"

	GUIDModeContent  = "content"
	GUIDModeRevision = "revision"

	MaxIgnorePatterns   = 50
	MaxIgnorePatternLen = 1000
)

// Config is the pagewatch-specific portion of scrape_sources.config.
type Config struct {
	IgnorePatterns  []string `json:"ignore_patterns,omitempty"`
	MinChangeChars  int      `json:"min_change_chars,omitempty"`
	WatchMode       string   `json:"watch_mode,omitempty"`
	IncludeFullBody *bool    `json:"include_full_body,omitempty"`
	GUIDMode        string   `json:"guid_mode,omitempty"`
	ScopeSelector   string   `json:"scope_selector,omitempty"` // reserved; unused in F0
}

// ParseConfig decodes and validates raw. An empty raw yields the zero
// (all-default) Config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("pagewatch: parse config: %w", err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if len(c.IgnorePatterns) > MaxIgnorePatterns {
		return fmt.Errorf("pagewatch: too many ignore_patterns (max %d)", MaxIgnorePatterns)
	}
	for _, p := range c.IgnorePatterns {
		if len(p) > MaxIgnorePatternLen {
			return fmt.Errorf("pagewatch: ignore_pattern too long (max %d chars)", MaxIgnorePatternLen)
		}
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("pagewatch: invalid ignore_pattern %q: %w", p, err)
		}
	}
	switch c.WatchMode {
	case "", WatchModeAdditions, WatchModeChanges:
	default:
		return fmt.Errorf("pagewatch: invalid watch_mode %q", c.WatchMode)
	}
	switch c.GUIDMode {
	case "", GUIDModeContent, GUIDModeRevision:
	default:
		return fmt.Errorf("pagewatch: invalid guid_mode %q", c.GUIDMode)
	}
	return nil
}

func (c Config) watchMode() string {
	if c.WatchMode == "" {
		return WatchModeAdditions
	}
	return c.WatchMode
}

func (c Config) includeFullBody() bool {
	if c.IncludeFullBody == nil {
		return true
	}
	return *c.IncludeFullBody
}

func (c Config) guidMode() string {
	if c.GUIDMode == "" {
		return GUIDModeContent
	}
	return c.GUIDMode
}

// configHash covers only the parts of Config that affect a block's
// comparison key (ignore_patterns). Fields that merely affect entry
// formatting (watch_mode, guid_mode, include_full_body) are excluded, since
// changing them doesn't invalidate saved state (§6.6).
func (c Config) configHash() string {
	norm, _ := json.Marshal(c.IgnorePatterns)
	return sha256Hex(norm)
}

func (c Config) compiledIgnorePatterns() ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(c.IgnorePatterns))
	for _, p := range c.IgnorePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}
