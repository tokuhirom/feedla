package selector

import "testing"

func TestParseConfigRequiresItemSelector(t *testing.T) {
	if _, err := ParseConfig(nil); err == nil {
		t.Fatal("expected error for missing config")
	}
	if _, err := ParseConfig([]byte(`{}`)); err == nil {
		t.Fatal("expected error for empty item_selector")
	}
}

func TestParseConfigRejectsInvalidSelector(t *testing.T) {
	_, err := ParseConfig([]byte(`{"item_selector": "article", "link_selector": "["}`))
	if err == nil {
		t.Fatal("expected error for invalid link_selector")
	}
}

func TestParseConfigRejectsOverlongSelector(t *testing.T) {
	long := make([]byte, MaxSelectorLen+1)
	for i := range long {
		long[i] = 'a'
	}
	cfg := []byte(`{"item_selector": "` + string(long) + `"}`)
	if _, err := ParseConfig(cfg); err == nil {
		t.Fatal("expected error for overlong item_selector")
	}
}

func TestParseConfigRejectsExcessiveMaxItemsPerCrawl(t *testing.T) {
	_, err := ParseConfig([]byte(`{"item_selector": "article", "max_items_per_crawl": 51}`))
	if err == nil {
		t.Fatal("expected error for max_items_per_crawl > 50")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"item_selector": "article"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if !cfg.SameHostOnly() {
		t.Error("SameHostOnly should default to true")
	}
	if !cfg.FulltextEnabled() {
		t.Error("FulltextEnabled should default to true")
	}
	if got := cfg.MaxItemsPerCrawlEffective(); got != DefaultMaxItemsPerCrawl {
		t.Errorf("MaxItemsPerCrawlEffective = %d, want %d", got, DefaultMaxItemsPerCrawl)
	}
}

func TestConfigOverrides(t *testing.T) {
	f := false
	cfg := Config{ItemSelector: "article", SameHostOnlyOpt: &f, FulltextOpt: &f, MaxItemsPerCrawl: 5}
	if cfg.SameHostOnly() {
		t.Error("SameHostOnly should be false")
	}
	if cfg.FulltextEnabled() {
		t.Error("FulltextEnabled should be false")
	}
	if got := cfg.MaxItemsPerCrawlEffective(); got != 5 {
		t.Errorf("MaxItemsPerCrawlEffective = %d, want 5", got)
	}
}
