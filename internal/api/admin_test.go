package api_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/crawler"
	"github.com/tokuhirom/feedla/internal/remotebackup"
	"github.com/tokuhirom/feedla/internal/store"
)

// newAdminTestServer is newTestServer's twin for admin-API tests: it also
// needs direct *store.Store access (createTestUser inserts rows directly,
// and tests look up other users' IDs) that newTestServer doesn't expose.
func newAdminTestServer(t *testing.T) (apiSrv *httptest.Server, st *store.Store, admin *http.Client) {
	t.Helper()
	return newAdminTestServerWithOptions(t, api.Options{})
}

// newAdminTestServerWithOptions is newAdminTestServer but lets the caller
// supply api.Options, e.g. to exercise BackupDir/BackupRemote-dependent
// endpoints.
func newAdminTestServerWithOptions(t *testing.T, opts api.Options) (apiSrv *httptest.Server, st *store.Store, admin *http.Client) {
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

	apiSrv = httptest.NewServer(api.NewHandler(st, cr, fetcher, nil, opts))
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

// fakeBackupLister is an api.BackupLister that records calls in-process
// instead of talking to real (or mock) object storage --
// internal/remotebackup's own tests already cover the S3 wire protocol
// against gofakes3, so here we only need to verify the handler surfaces
// whatever List returns.
type fakeBackupLister struct {
	objects []remotebackup.Object
	err     error
}

func (f *fakeBackupLister) List(context.Context) ([]remotebackup.Object, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objects, nil
}

type adminBackupStatusResponse struct {
	LocalEnabled bool `json:"local_enabled"`
	LocalDir     string
	LocalFiles   []struct {
		Name       string `json:"name"`
		SizeBytes  int64  `json:"size_bytes"`
		ModifiedAt int64  `json:"modified_at"`
	} `json:"local_files"`
	RemoteEnabled bool `json:"remote_enabled"`
	RemoteFiles   []struct {
		Name       string `json:"name"`
		SizeBytes  int64  `json:"size_bytes"`
		ModifiedAt int64  `json:"modified_at"`
	} `json:"remote_files"`
}

func TestAdminBackupStatusRequiresAdmin(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)
	member := createTestUser(t, st, apiSrv.URL, "member", false)

	if resp, err := member.Get(apiSrv.URL + "/api/v1/admin/backups"); err != nil {
		t.Fatalf("GET /admin/backups: %v", err)
	} else if resp.StatusCode != http.StatusForbidden {
		body, _ := decodeBody(resp)
		t.Fatalf("non-admin GET status = %d, want 403: %s", resp.StatusCode, body)
	}

	resp, err := admin.Get(apiSrv.URL + "/api/v1/admin/backups")
	if err != nil {
		t.Fatalf("GET /admin/backups: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("admin GET status = %d, want 200: %s", resp.StatusCode, body)
	}
	var out adminBackupStatusResponse
	decodeJSON(t, resp, &out)
	if out.LocalEnabled || out.RemoteEnabled {
		t.Fatalf("status = %+v, want both local and remote reported disabled (no BackupDir/BackupRemote configured)", out)
	}
}

func TestAdminBackupStatusListsLocalAndRemoteFiles(t *testing.T) {
	backupDir := t.TempDir()
	for _, name := range []string{"feedla-20260101.db", "feedla-20260101.opml", "feedla-20260215.db"} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("snapshot"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Non-backup files under the same dir shouldn't show up.
	if err := os.WriteFile(filepath.Join(backupDir, "not-a-backup.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	now := time.Now()
	remote := &fakeBackupLister{objects: []remotebackup.Object{
		{Key: "feedla/feedla-20260101.db", Size: 111, LastModified: now.Add(-24 * time.Hour)},
		{Key: "feedla/feedla-20260215.db", Size: 222, LastModified: now},
	}}

	apiSrv, _, admin := newAdminTestServerWithOptions(t, api.Options{BackupDir: backupDir, BackupRemote: remote})

	resp, err := admin.Get(apiSrv.URL + "/api/v1/admin/backups")
	if err != nil {
		t.Fatalf("GET /admin/backups: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var out adminBackupStatusResponse
	decodeJSON(t, resp, &out)

	if !out.LocalEnabled || !out.RemoteEnabled {
		t.Fatalf("status = %+v, want both local and remote reported enabled", out)
	}
	if len(out.LocalFiles) != 3 {
		t.Fatalf("local files = %+v, want 3 (stray non-backup file must be excluded)", out.LocalFiles)
	}
	if out.LocalFiles[0].Name != "feedla-20260215.db" {
		t.Fatalf("local files[0] = %q, want most-recent-first ordering", out.LocalFiles[0].Name)
	}
	if len(out.RemoteFiles) != 2 || out.RemoteFiles[0].Name != "feedla/feedla-20260215.db" {
		t.Fatalf("remote files = %+v, want most-recent-first ordering", out.RemoteFiles)
	}
}
