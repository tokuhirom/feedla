package api_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/store"
)

// newAdminTestServer is newTestServer's twin for admin-API tests: it also
// needs direct *store.Store access (createTestUser inserts rows directly,
// and tests look up other users' IDs) that newTestServer doesn't expose.
func newAdminTestServer(t *testing.T) (apiSrv *httptest.Server, st *store.Store, admin *http.Client) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "feedla.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	fetcher := crawler.NewFetcher(crawler.FetcherConfig{
		UserAgent:   "feedla-test/0.1",
		DialContext: (&net.Dialer{}).DialContext,
		HostSem:     crawler.NewHostSemaphore(4, 0),
	})
	cr := crawler.New(st, fetcher, 4, 0, 0)

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, api.Options{}))
	t.Cleanup(apiSrv.Close)
	admin = loginTestClient(t, apiSrv.URL)
	return apiSrv, st, admin
}

func TestAdminListUsersRequiresAdmin(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)
	member := createTestUser(t, st, apiSrv.URL, "member", false)

	if resp, err := member.Get(apiSrv.URL + "/api/v1/admin/users"); err != nil {
		t.Fatalf("GET /admin/users: %v", err)
	} else if resp.StatusCode != http.StatusForbidden {
		body, _ := decodeBody(resp)
		t.Fatalf("non-admin GET status = %d, want 403: %s", resp.StatusCode, body)
	}

	resp, err := admin.Get(apiSrv.URL + "/api/v1/admin/users")
	if err != nil {
		t.Fatalf("GET /admin/users: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("admin GET status = %d, want 200: %s", resp.StatusCode, body)
	}
	var out struct {
		Users []store.User `json:"users"`
	}
	decodeJSON(t, resp, &out)
	if len(out.Users) != 2 {
		t.Fatalf("got %d users, want 2 (bootstrap admin + member)", len(out.Users))
	}
}

func TestAdminCreateUser(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)

	// Non-admin can't create users either.
	member := createTestUser(t, st, apiSrv.URL, "member", false)
	if resp := postJSON(t, member, apiSrv.URL+"/api/v1/admin/users", map[string]any{
		"username": "nope", "password": otherUserTestPassword,
	}); resp.StatusCode != http.StatusForbidden {
		body, _ := decodeBody(resp)
		t.Fatalf("non-admin create status = %d, want 403: %s", resp.StatusCode, body)
	}

	resp := postJSON(t, admin, apiSrv.URL+"/api/v1/admin/users", map[string]any{
		"username": "newbie",
		"password": otherUserTestPassword,
		"is_admin": false,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("create user status = %d, want 201: %s", resp.StatusCode, body)
	}
	var created store.User
	decodeJSON(t, resp, &created)
	if created.Username != "newbie" || created.IsAdmin {
		t.Fatalf("created user = %+v, want newbie/non-admin", created)
	}

	// The new account can actually log in.
	loginResp := postJSON(t, member, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": "newbie",
		"password": otherUserTestPassword,
	})
	if loginResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(loginResp)
		t.Fatalf("login as newbie status = %d, want 200: %s", loginResp.StatusCode, body)
	}

	// Duplicate username conflicts.
	dupResp := postJSON(t, admin, apiSrv.URL+"/api/v1/admin/users", map[string]any{
		"username": "newbie",
		"password": otherUserTestPassword,
	})
	if dupResp.StatusCode != http.StatusConflict {
		body, _ := decodeBody(dupResp)
		t.Fatalf("duplicate username status = %d, want 409: %s", dupResp.StatusCode, body)
	}

	// Short password is rejected.
	shortResp := postJSON(t, admin, apiSrv.URL+"/api/v1/admin/users", map[string]any{
		"username": "shortpw",
		"password": "short",
	})
	if shortResp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(shortResp)
		t.Fatalf("short password status = %d, want 400: %s", shortResp.StatusCode, body)
	}
}

func TestAdminPatchUserDisableAndPromote(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)
	member := createTestUser(t, st, apiSrv.URL, "member", false)

	memberUser, err := st.GetUserByUsername(t.Context(), "member")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	// Promote member to admin.
	promoteURL := fmt.Sprintf("%s/api/v1/admin/users/%d", apiSrv.URL, memberUser.ID)
	promoteResp := patchJSON(t, admin, promoteURL, map[string]any{"is_admin": true})
	if promoteResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(promoteResp)
		t.Fatalf("promote status = %d, want 200: %s", promoteResp.StatusCode, body)
	}
	var promoted store.User
	decodeJSON(t, promoteResp, &promoted)
	if !promoted.IsAdmin {
		t.Fatalf("promoted user is_admin = false, want true")
	}

	// Disable member; their existing session must die immediately.
	disableResp := patchJSON(t, admin, promoteURL, map[string]any{"is_disabled": true})
	if disableResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(disableResp)
		t.Fatalf("disable status = %d, want 200: %s", disableResp.StatusCode, body)
	}
	if resp, err := member.Get(apiSrv.URL + "/api/v1/auth/me"); err != nil {
		t.Fatalf("GET /auth/me: %v", err)
	} else {
		var me struct {
			Authenticated bool `json:"authenticated"`
		}
		decodeJSON(t, resp, &me)
		if me.Authenticated {
			t.Fatalf("disabled user's session is still authenticated")
		}
	}

	// A disabled account can't log back in either.
	loginResp := postJSON(t, member, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": "member",
		"password": otherUserTestPassword,
	})
	if loginResp.StatusCode != http.StatusUnauthorized {
		body, _ := decodeBody(loginResp)
		t.Fatalf("disabled login status = %d, want 401: %s", loginResp.StatusCode, body)
	}
}

func TestAdminCannotRemoveLastAdmin(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)

	bootstrap, err := st.GetUserByUsername(t.Context(), testUsername)
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	selfURL := fmt.Sprintf("%s/api/v1/admin/users/%d", apiSrv.URL, bootstrap.ID)

	if resp := patchJSON(t, admin, selfURL, map[string]any{"is_admin": false}); resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("demote last admin status = %d, want 400: %s", resp.StatusCode, body)
	}
	if resp := patchJSON(t, admin, selfURL, map[string]any{"is_disabled": true}); resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("disable last admin status = %d, want 400: %s", resp.StatusCode, body)
	}

	// A second admin makes both operations legal again.
	createTestUser(t, st, apiSrv.URL, "second-admin", true)
	if resp := patchJSON(t, admin, selfURL, map[string]any{"is_admin": false}); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("demote with a second admin present status = %d, want 200: %s", resp.StatusCode, body)
	}
}
