package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/config"
	"github.com/tokuhirom/feedla/internal/inspect"
)

type inspectResponse struct {
	ViewURL   string            `json:"view_url"`
	Elements  []inspect.Element `json:"elements"`
	ExpiresAt int64             `json:"expires_at"`
}

func postInspect(t *testing.T, client *http.Client, apiSrvURL, targetURL string) inspectResponse {
	t.Helper()
	resp := postJSON(t, client, apiSrvURL+"/api/v1/scrape_sources/inspect", map[string]string{"url": targetURL})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("POST inspect status = %d, want 200: %s", resp.StatusCode, body)
	}
	var out inspectResponse
	decodeJSON(t, resp, &out)
	return out
}

// TestInspectViewServesSanitizedHTMLWithoutSession is the core property
// §8.3/§10.3 require: the sandboxed iframe that reads inspect/view back may
// not send a session cookie at all, so a bare, cookie-less client must
// still succeed on nothing but the token.
func TestInspectViewServesSanitizedHTMLWithoutSession(t *testing.T) {
	apiSrv, pageSrv, _, owner, _ := newTestServerWithPage(t, `<html><body>
		<article class="post"><h2>Title</h2><p>本文です。</p></article>
		<script>alert('evil')</script>
	</body></html>`)

	got := postInspect(t, owner, apiSrv.URL, pageSrv.URL)
	if got.ViewURL == "" {
		t.Fatalf("view_url is empty: %+v", got)
	}
	if len(got.Elements) == 0 {
		t.Fatalf("expected a non-empty element index, got: %+v", got)
	}

	// Deliberately a bare client with no cookie jar -- stands in for the
	// sandboxed iframe's cookie-less request.
	resp, err := (&http.Client{}).Get(apiSrv.URL + got.ViewURL)
	if err != nil {
		t.Fatalf("GET view_url (no session): %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("GET view_url status = %d, want 200: %s", resp.StatusCode, body)
	}

	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "sandbox allow-scripts", "script-src 'sha256-" + inspect.PickerScriptSHA256 + "'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP = %q, want it to contain %q", csp, want)
		}
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	body, err := decodeBody(resp)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(body, "alert('evil')") || strings.Contains(body, "<script>alert") {
		t.Errorf("expected the page's own <script> to be stripped, got: %s", body)
	}
	if !strings.Contains(body, "feedla-inspect-click") {
		t.Errorf("expected the picker script to be embedded, got: %s", body)
	}
	if !strings.Contains(body, "Title") || !strings.Contains(body, "本文です") {
		t.Errorf("expected allowed content to survive, got: %s", body)
	}
}

// TestInspectViewSingleUse covers §10.3's "使い捨て" requirement.
func TestInspectViewSingleUse(t *testing.T) {
	apiSrv, pageSrv, _, owner, _ := newTestServerWithPage(t, `<html><body><p>本文です。</p></body></html>`)
	got := postInspect(t, owner, apiSrv.URL, pageSrv.URL)

	client := &http.Client{}
	resp, err := client.Get(apiSrv.URL + got.ViewURL)
	if err != nil {
		t.Fatalf("first GET view_url: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first GET view_url status = %d, want 200", resp.StatusCode)
	}

	resp, err = client.Get(apiSrv.URL + got.ViewURL)
	if err != nil {
		t.Fatalf("second GET view_url: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second GET view_url status = %d, want 404 (single use)", resp.StatusCode)
	}
}

// TestInspectViewCrossUserSessionRejected is the defense-in-depth half of
// the token-only design (see handleInspectView's doc comment / CLAUDE.md's
// IDOR-test requirement): a *different, authenticated* user presenting the
// same token gets rejected, even though the token alone would otherwise be
// enough. The main text of the token-only contract --
// TestInspectViewServesSanitizedHTMLWithoutSession above -- already proves
// the no-cookie legitimate path isn't blocked by this check.
func TestInspectViewCrossUserSessionRejected(t *testing.T) {
	apiSrv, pageSrv, _, owner, st := newTestServerWithPage(t, `<html><body><p>本文です。</p></body></html>`)
	got := postInspect(t, owner, apiSrv.URL, pageSrv.URL)

	other := createTestUser(t, st, apiSrv.URL, "inspect-other-user", false)
	resp, err := other.Get(apiSrv.URL + got.ViewURL)
	if err != nil {
		t.Fatalf("GET view_url as other user: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET view_url as other user status = %d, want 404", resp.StatusCode)
	}
}

func TestInspectScrapeSourceRequiresAuth(t *testing.T) {
	apiSrv, pageSrv, _, _, _ := newTestServerWithPage(t, `<html><body><p>x</p></body></html>`)

	resp := postJSON(t, &http.Client{}, apiSrv.URL+"/api/v1/scrape_sources/inspect", map[string]string{"url": pageSrv.URL})
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 401: %s", resp.StatusCode, body)
	}
}

func TestInspectScrapeSourceMissingURL(t *testing.T) {
	apiSrv, _, _, owner, _ := newTestServerWithPage(t, `<html><body><p>x</p></body></html>`)

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/scrape_sources/inspect", map[string]string{})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestInspectScrapeSourceRateLimited(t *testing.T) {
	apiSrv, pageSrv, client, _ := newTestServerWithPageOptions(t, `<html><body><p>本文です。</p></body></html>`, api.Options{
		Quota: config.Quota{MaxSubscriptions: 100, MaxScrapeSources: 100, FeedAddPerHour: 100, PreviewPerHour: 1},
	})

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources/inspect", map[string]string{"url": pageSrv.URL})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("first inspect status = %d, want 200: %s", resp.StatusCode, body)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/scrape_sources/inspect", map[string]string{"url": pageSrv.URL})
	if resp.StatusCode != http.StatusTooManyRequests {
		body, _ := decodeBody(resp)
		t.Fatalf("second inspect (over PreviewPerHour=1, shared with preview) status = %d, want 429: %s", resp.StatusCode, body)
	}
}
