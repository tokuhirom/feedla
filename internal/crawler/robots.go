package crawler

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

const robotsCacheTTL = 24 * time.Hour

// robotsRule is one Disallow/Allow line from a matched User-agent group.
type robotsRule struct {
	disallow bool
	pattern  string
}

type robotsGroup struct {
	agents []string // lowercased user-agent tokens this group applies to
	rules  []robotsRule
}

// RobotsCache fetches and caches robots.txt per host, deciding whether an
// individual article URL may be fetched (§7.4 of
// docs/feedless-site-subscription-selector.md). It only judges individual
// article fetches — the listing page itself is a URL the user explicitly
// registered, so it is never checked here, matching F0's treatment of its
// single watched page.
//
// Implements the de-facto subset of the robots.txt convention feedla needs:
// per-User-agent Disallow/Allow with longest-match-wins and */$ wildcards.
// Crawl-delay is intentionally ignored — HostSemaphore already enforces a
// minimum 1s gap between requests to the same host, so honoring a larger
// Crawl-delay would only ever extend the crawl-pool occupancy time (§7.3)
// for no politeness benefit.
type RobotsCache struct {
	mu      sync.Mutex
	entries map[string]*robotsCacheEntry
	now     func() time.Time
}

type robotsCacheEntry struct {
	rules     []robotsRule // pre-selected for our own UA token; nil = no restrictions
	expiresAt time.Time
}

func NewRobotsCache() *RobotsCache {
	return &RobotsCache{entries: map[string]*robotsCacheEntry{}, now: time.Now}
}

// Allowed reports whether fetching articleURL is permitted by its host's
// robots.txt for the given User-Agent header value. Fetch failures,
// non-200 responses — including 5xx, an intentional deviation from RFC 9309
// since a self-hosted tool going quiet because a site is briefly unwell is
// worse than the alternative — and parse errors are all treated as "no
// restriction".
func (c *RobotsCache) Allowed(ctx context.Context, fetcher *Fetcher, userAgent, articleURL string) bool {
	u, err := url.Parse(articleURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return true
	}
	key := u.Scheme + "://" + u.Host
	rules := c.getRules(ctx, fetcher, userAgent, key)
	if rules == nil {
		return true
	}
	return matchRules(rules, u.EscapedPath())
}

func (c *RobotsCache) getRules(ctx context.Context, fetcher *Fetcher, userAgent, hostKey string) []robotsRule {
	now := c.now()

	c.mu.Lock()
	if e, ok := c.entries[hostKey]; ok && now.Before(e.expiresAt) {
		c.mu.Unlock()
		return e.rules
	}
	c.mu.Unlock()

	rules := fetchRobotsRules(ctx, fetcher, userAgent, hostKey)

	c.mu.Lock()
	c.entries[hostKey] = &robotsCacheEntry{rules: rules, expiresAt: now.Add(robotsCacheTTL)}
	c.mu.Unlock()
	return rules
}

func fetchRobotsRules(ctx context.Context, fetcher *Fetcher, userAgent, hostKey string) []robotsRule {
	result, err := fetcher.Fetch(ctx, hostKey+"/robots.txt", FetchOptions{})
	if err != nil || result.StatusCode != 200 {
		return nil
	}
	groups := parseRobotsTxt(string(result.Body))
	g := selectGroup(groups, productToken(userAgent))
	if g == nil {
		return nil
	}
	return g.rules
}

// productToken extracts the product token feedla's own User-Agent string
// should be matched against: everything up to the first "/" or space (§7.4).
func productToken(userAgent string) string {
	ua := userAgent
	if i := strings.IndexAny(ua, "/ "); i >= 0 {
		ua = ua[:i]
	}
	return ua
}

func parseRobotsTxt(body string) []robotsGroup {
	var groups []robotsGroup
	var current *robotsGroup
	startedRules := false

	for _, rawLine := range strings.Split(body, "\n") {
		line := rawLine
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:i]))
		val := strings.TrimSpace(line[i+1:])

		switch key {
		case "user-agent":
			if current == nil || startedRules {
				groups = append(groups, robotsGroup{})
				current = &groups[len(groups)-1]
				startedRules = false
			}
			current.agents = append(current.agents, strings.ToLower(val))
		case "disallow":
			if current == nil {
				continue
			}
			startedRules = true
			if val != "" {
				current.rules = append(current.rules, robotsRule{disallow: true, pattern: val})
			}
		case "allow":
			if current == nil {
				continue
			}
			startedRules = true
			if val != "" {
				current.rules = append(current.rules, robotsRule{disallow: false, pattern: val})
			}
		}
	}
	return groups
}

// selectGroup picks the group whose agents include uaToken exactly
// (case-insensitive), falling back to the wildcard "*" group.
func selectGroup(groups []robotsGroup, uaToken string) *robotsGroup {
	uaLower := strings.ToLower(uaToken)
	for i := range groups {
		for _, a := range groups[i].agents {
			if a == uaLower {
				return &groups[i]
			}
		}
	}
	for i := range groups {
		for _, a := range groups[i].agents {
			if a == "*" {
				return &groups[i]
			}
		}
	}
	return nil
}

// matchRules applies longest-match-wins across every rule whose pattern
// matches path; ties are not expected in practice (patterns of equal length
// are almost always identical), so the first one encountered wins.
func matchRules(rules []robotsRule, path string) bool {
	bestLen := -1
	bestDisallow := false
	for _, r := range rules {
		if !pathMatches(r.pattern, path) {
			continue
		}
		if l := len(r.pattern); l > bestLen {
			bestLen = l
			bestDisallow = r.disallow
		}
	}
	if bestLen < 0 {
		return true
	}
	return !bestDisallow
}

// pathMatches implements the de-facto robots.txt pattern language: literal
// prefix matching, "*" as a wildcard matching any sequence (including
// empty), and a trailing "$" anchoring the match to the end of path.
func pathMatches(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	p := strings.TrimSuffix(pattern, "$")
	if p == "" {
		return !anchored || path == ""
	}
	segs := strings.Split(p, "*")
	pos := 0
	for i, seg := range segs {
		if seg == "" {
			continue
		}
		idx := strings.Index(path[pos:], seg)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(seg)
	}
	if anchored {
		return pos == len(path)
	}
	return true
}
