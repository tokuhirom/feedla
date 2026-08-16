package api_test

import (
	"net/http"
	"testing"
)

func TestCSRFOriginCheck(t *testing.T) {
	apiSrv, feedSrv := newTestServer(t)

	url := apiSrv.URL + "/api/v1/subscriptions"

	t.Run("missing origin is allowed (non-browser client)", func(t *testing.T) {
		resp := postJSON(t, url, map[string]string{"url": feedSrv.URL})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
	})

	t.Run("matching origin is allowed", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", apiSrv.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Body = http.NoBody
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		// Empty body decodes to a request with url == "", which is a
		// validation error further down the handler chain, not a CSRF
		// rejection — we only care that it wasn't blocked at 403.
		if resp.StatusCode == http.StatusForbidden {
			t.Fatalf("status = %d, want anything but 403 (same-origin request)", resp.StatusCode)
		}
	})

	t.Run("cross-origin request is rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "application/json")
		req.Body = http.NoBody
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("GET is never blocked regardless of origin", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, apiSrv.URL+"/api/v1/subscriptions", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("LDR-compatible form endpoint rejects foreign origin", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, apiSrv.URL+"/api/subscribe", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Body = http.NoBody
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})
}
