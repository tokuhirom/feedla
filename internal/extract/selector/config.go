package selector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/andybalholm/cascadia"
)

const (
	MaxSelectorLen = 200

	DefaultMaxItemsPerCrawl = 20
	MaxMaxItemsPerCrawl     = 50

	// MaxCandidates caps how many item_selector matches are considered per
	// crawl (§4.3). Excess matches are dropped, document order, and
	// State.Truncated is set.
	MaxCandidates = 500

	// MaxSeen caps scrape_sources.state's seen list (§6.1.1).
	MaxSeen = 2000

	// MaxArticleRetries is how many times an individual article fetch may
	// fail before crawler gives up and imports it title-only (§4.5).
	MaxArticleRetries = 3
)

// Config is the selector-specific portion of scrape_sources.config (方式
// B1). ItemSelector is required; the rest are optional relative selectors
// evaluated against each item element, or booleans/ints with the documented
// defaults.
type Config struct {
	ItemSelector     string `json:"item_selector"`
	LinkSelector     string `json:"link_selector,omitempty"`
	TitleSelector    string `json:"title_selector,omitempty"`
	DateSelector     string `json:"date_selector,omitempty"`
	SummarySelector  string `json:"summary_selector,omitempty"`
	SameHostOnlyOpt  *bool  `json:"same_host_only,omitempty"` // default true
	FulltextOpt      *bool  `json:"fulltext,omitempty"`       // default true
	MaxItemsPerCrawl int    `json:"max_items_per_crawl,omitempty"`
}

// ParseConfig decodes and validates raw. Unlike pagewatch, an empty/missing
// raw is an error: item_selector has no meaningful default.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return Config{}, fmt.Errorf("selector: item_selector is required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("selector: parse config: %w", err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.ItemSelector == "" {
		return fmt.Errorf("selector: item_selector is required")
	}
	selectors := []struct {
		name, val string
	}{
		{"item_selector", c.ItemSelector},
		{"link_selector", c.LinkSelector},
		{"title_selector", c.TitleSelector},
		{"date_selector", c.DateSelector},
		{"summary_selector", c.SummarySelector},
	}
	for _, s := range selectors {
		if s.val == "" {
			continue
		}
		if len(s.val) > MaxSelectorLen {
			return fmt.Errorf("selector: %s too long (max %d chars)", s.name, MaxSelectorLen)
		}
		if _, err := cascadia.Parse(s.val); err != nil {
			return fmt.Errorf("selector: invalid %s %q: %w", s.name, s.val, err)
		}
	}
	if c.MaxItemsPerCrawl < 0 {
		return fmt.Errorf("selector: max_items_per_crawl must not be negative")
	}
	if c.MaxItemsPerCrawl > MaxMaxItemsPerCrawl {
		return fmt.Errorf("selector: max_items_per_crawl too large (max %d)", MaxMaxItemsPerCrawl)
	}
	return nil
}

// SameHostOnly reports whether candidate links outside the list page's host
// should be discarded (§4.3 step 5). Default true.
func (c Config) SameHostOnly() bool {
	if c.SameHostOnlyOpt == nil {
		return true
	}
	return *c.SameHostOnlyOpt
}

// FulltextEnabled reports whether crawler should fetch each new article's
// page and extract its main content (§4.5). Default true.
func (c Config) FulltextEnabled() bool {
	if c.FulltextOpt == nil {
		return true
	}
	return *c.FulltextOpt
}

// MaxItemsPerCrawl returns the effective per-crawl article-fetch cap,
// applying the default and upper bound (§7.3).
func (c Config) MaxItemsPerCrawlEffective() int {
	if c.MaxItemsPerCrawl <= 0 {
		return DefaultMaxItemsPerCrawl
	}
	if c.MaxItemsPerCrawl > MaxMaxItemsPerCrawl {
		return MaxMaxItemsPerCrawl
	}
	return c.MaxItemsPerCrawl
}

// ConfigHash is a stable hash of the full selector config, exposed for
// display only (§6.1: "セレクタを変更してから再クロールされたかの表示")  —
// it does not drive any extraction or state-invalidation behavior.
func (c Config) ConfigHash() string {
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
