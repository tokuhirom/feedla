package crawler

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPathMatchesLongestMatchWins(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/private/", "/private/x", true},
		{"/private/", "/public/x", false},
		{"/a*c", "/abc", true},
		{"/a*c", "/abxc", true},
		{"/a*c", "/ab", false},
		{"/page$", "/page", true},
		{"/page$", "/page2", false},
		{"", "/anything", true}, // empty Disallow pattern never appears as a rule, but pathMatches("", x) shouldn't panic
	}
	for _, c := range cases {
		if got := pathMatches(c.pattern, c.path); got != c.want {
			t.Errorf("pathMatches(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMatchRulesLongestWins(t *testing.T) {
	rules := []robotsRule{
		{disallow: true, pattern: "/"},
		{disallow: false, pattern: "/public/"},
	}
	if !matchRules(rules, "/public/x") {
		t.Error("more specific Allow should win over a blanket Disallow")
	}
	if matchRules(rules, "/private/x") {
		t.Error("blanket Disallow should block unmatched paths")
	}
}

func TestMatchRulesNoMatchAllowsByDefault(t *testing.T) {
	rules := []robotsRule{{disallow: true, pattern: "/private/"}}
	if !matchRules(rules, "/public/x") {
		t.Error("a path matching no rule should be allowed")
	}
}

func TestParseRobotsTxtSelectsUAGroup(t *testing.T) {
	body := `
User-agent: BadBot
Disallow: /

User-agent: feedla
Disallow: /private/
Allow: /private/public-ish/

User-agent: *
Disallow: /everything/
`
	groups := parseRobotsTxt(body)
	g := selectGroup(groups, "feedla")
	if g == nil {
		t.Fatal("expected to find the feedla group")
	}
	if len(g.rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(g.rules))
	}

	// A UA with no dedicated group falls back to "*".
	g2 := selectGroup(groups, "SomeOtherBot")
	if g2 == nil || len(g2.rules) != 1 || g2.rules[0].pattern != "/everything/" {
		t.Errorf("expected fallback to * group, got %+v", g2)
	}
}

func TestProductToken(t *testing.T) {
	cases := map[string]string{
		"feedla/1.0 (+https://example.com)": "feedla",
		"feedla 1.0":                        "feedla",
		"feedla":                            "feedla",
	}
	for ua, want := range cases {
		if got := productToken(ua); got != want {
			t.Errorf("productToken(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestRobotsCacheAllowedDisallowsMatchedPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := NewFetcher(FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     NewHostSemaphore(4, 0),
	})
	cache := NewRobotsCache()

	if cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/private/secret") {
		t.Error("expected /private/ to be disallowed")
	}
	if !cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/public/ok") {
		t.Error("expected /public/ to be allowed")
	}
}

func TestRobotsCacheNoRobotsTxtAllowsEverything(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := NewFetcher(FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     NewHostSemaphore(4, 0),
	})
	cache := NewRobotsCache()

	if !cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/anything") {
		t.Error("a 404 robots.txt should mean no restriction")
	}
}

func TestRobotsCacheCaches(t *testing.T) {
	var fetchCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := NewFetcher(FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     NewHostSemaphore(4, 0),
	})
	fakeNow := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	cache := &RobotsCache{entries: map[string]*robotsCacheEntry{}, now: func() time.Time { return fakeNow }}

	cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/x")
	cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/y")
	if fetchCount != 1 {
		t.Errorf("robots.txt fetched %d times, want 1 (cached)", fetchCount)
	}

	fakeNow = fakeNow.Add(25 * time.Hour)
	cache.Allowed(t.Context(), f, "feedla-test/0.1", srv.URL+"/z")
	if fetchCount != 2 {
		t.Errorf("robots.txt fetched %d times after TTL expiry, want 2", fetchCount)
	}
}
