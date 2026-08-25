package api_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestGzipMiddleware_CompressesWhenAccepted covers the fix for a slow GET
// /api/v1/subscriptions on large accounts: the server itself answered in
// tens of milliseconds, but transferring an uncompressed JSON body over
// 1000+ subscriptions took over a second -- see gzip_middleware.go.
func TestGzipMiddleware_CompressesWhenAccepted(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	req, err := http.NewRequest(http.MethodGet, apiSrv.URL+"/api/v1/subscriptions", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Setting Accept-Encoding explicitly stops net/http's Transport from
	// auto-negotiating gzip and transparently decompressing the response --
	// exactly the setup needed to observe Content-Encoding ourselves.
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "gzip")
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()

	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzipped body: %v", err)
	}

	var decoded struct {
		Subscriptions []any `json:"subscriptions"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal decompressed body: %v (body: %s)", err, body)
	}
}

// TestGzipMiddleware_SkipsWhenNotAccepted ensures a client that doesn't
// advertise gzip support (e.g. a plain curl invocation) still gets a
// readable, uncompressed body.
func TestGzipMiddleware_SkipsWhenNotAccepted(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	req, err := http.NewRequest(http.MethodGet, apiSrv.URL+"/api/v1/subscriptions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}

	var decoded struct {
		Subscriptions []any `json:"subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}
