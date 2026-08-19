package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/remotebackup"
)

type authMeBody struct {
	Authenticated bool `json:"authenticated"`
	SetupRequired bool `json:"setup_required"`
	User          *struct {
		ID                     int64  `json:"id"`
		Username               string `json:"username"`
		IsAdmin                bool   `json:"is_admin"`
		InstagramEmbedsEnabled bool   `json:"instagram_embeds_enabled"`
	} `json:"user"`
	RestoreHint *struct {
		LocalConfigured      bool   `json:"local_configured"`
		LocalHasSnapshot     bool   `json:"local_has_snapshot"`
		RemoteConfigured     bool   `json:"remote_configured"`
		RemoteHasSnapshot    bool   `json:"remote_has_snapshot"`
		RemoteError          bool   `json:"remote_error"`
		RestoreSupported     bool   `json:"restore_supported"`
		LatestSnapshot       string `json:"latest_snapshot"`
		LatestSnapshotSource string `json:"latest_snapshot_source"`
	} `json:"restore_hint"`
}

func getMe(t *testing.T, client *http.Client, apiSrvURL string) authMeBody {
	t.Helper()
	resp, err := client.Get(apiSrvURL + "/api/v1/auth/me")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/me: %v", err)
	}
	var body authMeBody
	decodeJSON(t, resp, &body)
	return body
}

// freshTestServer is newTestServer's twin for auth-flow tests that need to
// drive setup/login themselves: it returns a server with setup still
// pending and a client with an empty cookie jar, instead of newTestServer's
// automatic loginTestClient bootstrap.
func freshTestServer(t *testing.T) (apiSrv, feedSrv *httptest.Server, client *http.Client) {
	t.Helper()
	apiSrv, feedSrv = newTestServerNoLogin(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return apiSrv, feedSrv, &http.Client{Jar: jar}
}

func newClientForServer() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// TestAuthMeRestoreHint_NoneConfigured covers the plain "fresh install,
// no backup config at all" case: both local/remote report unconfigured,
// so the setup screen has no restore to explain away.
func TestAuthMeRestoreHint_NoneConfigured(t *testing.T) {
	apiSrv, _, freshClient := freshTestServer(t)

	me := getMe(t, freshClient, apiSrv.URL)
	if me.RestoreHint == nil {
		t.Fatalf("me = %+v, want restore_hint present while setup is pending", me)
	}
	if me.RestoreHint.LocalConfigured || me.RestoreHint.RemoteConfigured {
		t.Fatalf("restore_hint = %+v, want both local/remote unconfigured", me.RestoreHint)
	}
}

// TestAuthMeRestoreHint_ConfiguredButEmpty covers the case this feature
// exists for: FR_BACKUP_DIR / FR_BACKUP_REMOTE_* are set, but the fresh
// deploy target's dirs/bucket genuinely have nothing to restore from yet --
// distinct from "not configured at all" and from "misconfigured".
func TestAuthMeRestoreHint_ConfiguredButEmpty(t *testing.T) {
	backupDir := t.TempDir()
	remote := &fakeBackupLister{}

	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{BackupDir: backupDir, BackupRemote: remote})
	freshClient := newClientForServer()

	me := getMe(t, freshClient, apiSrv.URL)
	if me.RestoreHint == nil {
		t.Fatalf("me = %+v, want restore_hint present", me)
	}
	hint := me.RestoreHint
	if !hint.LocalConfigured || hint.LocalHasSnapshot {
		t.Fatalf("restore_hint local = %+v, want configured with no snapshot", hint)
	}
	if !hint.RemoteConfigured || hint.RemoteHasSnapshot || hint.RemoteError {
		t.Fatalf("restore_hint remote = %+v, want configured with no snapshot and no error", hint)
	}
}

// TestAuthMeRestoreHint_RemoteError covers a misconfigured remote (bad
// endpoint/credentials): the hint must distinguish this from "configured
// but genuinely empty" so an operator knows to check FR_BACKUP_REMOTE_*
// rather than assume the bucket is just empty.
func TestAuthMeRestoreHint_RemoteError(t *testing.T) {
	remote := &fakeBackupLister{err: fmt.Errorf("boom")}

	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{BackupRemote: remote})
	freshClient := newClientForServer()

	me := getMe(t, freshClient, apiSrv.URL)
	if me.RestoreHint == nil || !me.RestoreHint.RemoteConfigured || !me.RestoreHint.RemoteError {
		t.Fatalf("restore_hint = %+v, want remote configured with an error", me.RestoreHint)
	}
}

func TestSetupFlow(t *testing.T) {
	apiSrv, _, freshClient := freshTestServer(t)

	me := getMe(t, freshClient, apiSrv.URL)
	if me.Authenticated || !me.SetupRequired {
		t.Fatalf("me before setup = %+v, want authenticated=false setup_required=true", me)
	}

	// Too-short password rejected.
	resp := postJSON(t, freshClient, apiSrv.URL+"/api/v1/auth/setup", map[string]string{
		"username": "admin", "password": "short",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("setup with short password status = %d, want 400", resp.StatusCode)
	}

	resp = postJSON(t, freshClient, apiSrv.URL+"/api/v1/auth/setup", map[string]string{
		"username": testUsername, "password": testPassword,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("setup status = %d, want 200: %s", resp.StatusCode, body)
	}

	me = getMe(t, freshClient, apiSrv.URL)
	if !me.Authenticated || me.User == nil || me.User.Username != testUsername || !me.User.IsAdmin {
		t.Fatalf("me after setup = %+v", me)
	}

	// Setup can never run again.
	resp = postJSON(t, freshClient, apiSrv.URL+"/api/v1/auth/setup", map[string]string{
		"username": "someone-else", "password": testPassword,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup status = %d, want 409", resp.StatusCode)
	}
}

func TestUnauthenticatedRequestIsRejected(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	anon := newClientForServer() // no session cookie at all
	resp, err := anon.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions (anonymous): %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	// Sanity check the authenticated client actually works, so the 401
	// above is meaningfully about auth and not some other breakage.
	resp, err = client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions (authenticated): %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", resp.StatusCode)
	}
}

func TestLoginWrongPasswordAndLogout(t *testing.T) {
	apiSrv, _, setupClient := freshTestServer(t)
	resp := postJSON(t, setupClient, apiSrv.URL+"/api/v1/auth/setup", map[string]string{
		"username": testUsername, "password": testPassword,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status = %d, want 200", resp.StatusCode)
	}

	// Unknown user and wrong password get the same treatment (status,
	// message shape), so a client can't distinguish "no such account" from
	// "wrong password" -- checked against usernames that never attempt the
	// real login below, so the account-level backoff (see
	// TestLoginRateLimitPerAccount) doesn't interfere with it.
	resp = postJSON(t, newClientForServer(), apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": "wrong-password-account", "password": "totally-wrong-password",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", resp.StatusCode)
	}
	resp = postJSON(t, newClientForServer(), apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": "no-such-user", "password": testPassword,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want 401 (same as wrong password)", resp.StatusCode)
	}

	loginClient := newClientForServer()
	resp = postJSON(t, loginClient, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": testUsername, "password": testPassword,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("correct login status = %d, want 200: %s", resp.StatusCode, body)
	}

	resp, err := loginClient.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after login = %d, want 200", resp.StatusCode)
	}

	resp = doWithOrigin(t, loginClient, http.MethodPost, apiSrv.URL+"/api/v1/auth/logout", "application/json", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}

	resp, err = loginClient.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions after logout: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want 401", resp.StatusCode)
	}
}

func TestLoginRateLimitPerAccount(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": testUsername, "password": "wrong-1",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("first wrong attempt status = %d, want 401", resp.StatusCode)
	}

	// The account-level exponential backoff kicks in immediately after one
	// failure, so a second attempt right away must be rate-limited rather
	// than re-checking the password.
	resp = postJSON(t, client, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": testUsername, "password": testPassword,
	})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("immediate retry status = %d, want 429 (account backoff)", resp.StatusCode)
	}
}

func TestChangePasswordInvalidatesSessions(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/auth/password", map[string]string{
		"current": "wrong-current-password", "new": "brand-new-password-1",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", resp.StatusCode)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/auth/password", map[string]string{
		"current": testPassword, "new": "brand-new-password-1",
	})
	if resp.StatusCode != http.StatusNoContent {
		body, _ := decodeBody(resp)
		t.Fatalf("change password status = %d, want 204: %s", resp.StatusCode, body)
	}

	// The session used to change the password is itself invalidated.
	resp, err := client.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions after password change: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after password change = %d, want 401", resp.StatusCode)
	}

	// The new password logs in fine.
	newClient := newClientForServer()
	resp = postJSON(t, newClient, apiSrv.URL+"/api/v1/auth/login", map[string]string{
		"username": testUsername, "password": "brand-new-password-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with new password status = %d, want 200", resp.StatusCode)
	}
}

func TestUpdateMeInstagramEmbedsEnabled(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	me := getMe(t, client, apiSrv.URL)
	if me.User == nil || me.User.InstagramEmbedsEnabled {
		t.Fatalf("initial instagram_embeds_enabled = %+v, want false by default", me.User)
	}

	resp := patchJSON(t, client, apiSrv.URL+"/api/v1/auth/me", map[string]any{
		"instagram_embeds_enabled": true,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("PATCH /api/v1/auth/me status = %d, want 200: %s", resp.StatusCode, body)
	}

	me = getMe(t, client, apiSrv.URL)
	if me.User == nil || !me.User.InstagramEmbedsEnabled {
		t.Fatalf("instagram_embeds_enabled after PATCH = %+v, want true", me.User)
	}

	// Missing field is a 400, not "leave it unchanged" -- there's only one
	// field today, but the request shape shouldn't silently no-op.
	resp = patchJSON(t, client, apiSrv.URL+"/api/v1/auth/me", map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(resp)
		t.Fatalf("PATCH with empty body status = %d, want 400: %s", resp.StatusCode, body)
	}

	// Unauthenticated requests are rejected (no session/token cookie jar).
	resp = patchJSON(t, newClientForServer(), apiSrv.URL+"/api/v1/auth/me", map[string]any{
		"instagram_embeds_enabled": true,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := decodeBody(resp)
		t.Fatalf("unauthenticated PATCH status = %d, want 401: %s", resp.StatusCode, body)
	}
}

// TestUpdateMeInstagramEmbedsEnabledIsPerUser covers the cross-user
// isolation angle CLAUDE.md's セキュリティ節 asks for: handleAuthUpdateMe
// only ever writes userFromContext's own row, so a second user's toggle
// must never leak into the first user's setting.
func TestUpdateMeInstagramEmbedsEnabledIsPerUser(t *testing.T) {
	apiSrv, _, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})
	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	resp := patchJSON(t, other, apiSrv.URL+"/api/v1/auth/me", map[string]any{
		"instagram_embeds_enabled": true,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("other user PATCH status = %d, want 200: %s", resp.StatusCode, body)
	}

	ownerMe := getMe(t, owner, apiSrv.URL)
	if ownerMe.User == nil || ownerMe.User.InstagramEmbedsEnabled {
		t.Fatalf("owner instagram_embeds_enabled = %+v, want unaffected by other user's PATCH", ownerMe.User)
	}

	otherMe := getMe(t, other, apiSrv.URL)
	if otherMe.User == nil || !otherMe.User.InstagramEmbedsEnabled {
		t.Fatalf("other user instagram_embeds_enabled = %+v, want true", otherMe.User)
	}
}

func TestCSRFOriginCheck(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	t.Run("missing origin on cookie-authenticated POST is rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, apiSrv.URL+"/api/v1/subscriptions", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Body = http.NoBody
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (missing Origin)", resp.StatusCode)
		}
	})

	t.Run("cross-origin request is rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, apiSrv.URL+"/api/v1/subscriptions", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "application/json")
		req.Body = http.NoBody
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("matching origin is allowed", func(t *testing.T) {
		resp := subscribe(t, client, apiSrv.URL, feedSrv.URL)
		if resp.StatusCode != http.StatusCreated {
			body, _ := decodeBody(resp)
			t.Fatalf("status = %d, want 201: %s", resp.StatusCode, body)
		}
	})

	t.Run("GET is never blocked regardless of origin", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, apiSrv.URL+"/api/v1/subscriptions", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://evil.example")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestAPITokenAuthBypassesCSRFAndCookies(t *testing.T) {
	apiSrv, feedSrv, client := newTestServer(t)

	createResp := postJSON(t, client, apiSrv.URL+"/api/v1/auth/tokens", map[string]string{"label": "test client"})
	if createResp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(createResp)
		t.Fatalf("create token status = %d, want 201: %s", createResp.StatusCode, body)
	}
	var created struct {
		Token string `json:"token"`
	}
	decodeJSON(t, createResp, &created)
	if created.Token == "" {
		t.Fatal("want a non-empty raw token")
	}

	// A bare client with no cookie jar at all, no Origin header: this is
	// exactly the "curl-like" non-browser client the token path exists for.
	body := strings.NewReader(`{"url":"` + feedSrv.URL + `"}`)
	req, err := http.NewRequest(http.MethodPost, apiSrv.URL+"/api/v1/subscriptions", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+created.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("token-authenticated request: %v", err)
	}
	// 202 (candidates), not 403/401: proves the request reached the
	// handler at all -- CSRF/cookie enforcement is what this test is
	// actually about, not the subscribe flow's discover/confirm shape.
	if resp.StatusCode != http.StatusAccepted {
		b, _ := decodeBody(resp)
		t.Fatalf("status = %d, want 202 (token auth, no Origin needed): %s", resp.StatusCode, b)
	}

	// An invalid token is rejected like any other unauthenticated request.
	req2, _ := http.NewRequest(http.MethodGet, apiSrv.URL+"/api/v1/subscriptions", nil)
	req2.Header.Set("Authorization", "Bearer not-a-real-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("bad token request: %v", err)
	}
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", resp2.StatusCode)
	}
}

func TestAPITokenListAndDelete(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	createResp := postJSON(t, client, apiSrv.URL+"/api/v1/auth/tokens", map[string]string{"label": "a"})
	var created struct {
		Info struct {
			ID int64 `json:"id"`
		} `json:"info"`
	}
	decodeJSON(t, createResp, &created)

	listResp, err := client.Get(apiSrv.URL + "/api/v1/auth/tokens")
	if err != nil {
		t.Fatalf("GET tokens: %v", err)
	}
	var listed struct {
		Tokens []struct {
			ID    int64  `json:"id"`
			Label string `json:"label"`
		} `json:"tokens"`
	}
	decodeJSON(t, listResp, &listed)
	if len(listed.Tokens) != 1 || listed.Tokens[0].Label != "a" {
		t.Fatalf("tokens = %+v, want one labeled 'a'", listed.Tokens)
	}

	resp := deleteReq(t, client, fmt.Sprintf("%s/api/v1/auth/tokens/%d", apiSrv.URL, created.Info.ID))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete token status = %d, want 204", resp.StatusCode)
	}

	resp = deleteReq(t, client, fmt.Sprintf("%s/api/v1/auth/tokens/%d", apiSrv.URL, created.Info.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete already-deleted token status = %d, want 404", resp.StatusCode)
	}
}

// TestAuthMeRestoreHint_LatestSnapshot verifies the hint reports the newest
// .db snapshot across local and remote (base names embed YYYYMMDD, so the
// remote 0215 beats the local 0101 here), which is what the setup screen's
// restore choice offers to restore.
func TestAuthMeRestoreHint_LatestSnapshot(t *testing.T) {
	backupDir := t.TempDir()
	for _, name := range []string{"feedla-20260101.db", "feedla-20260101.opml"} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	remote := &fakeBackupLister{objects: []remotebackup.Object{
		{Key: "feedla/feedla-20260215.db", Size: 1, LastModified: time.Now()},
		{Key: "feedla/feedla-20260215.opml", Size: 1, LastModified: time.Now()},
	}}

	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{
		BackupDir:    backupDir,
		BackupRemote: remote,
		SetupRestore: func(context.Context) error { return nil },
	})
	me := getMe(t, newClientForServer(), apiSrv.URL)
	if me.RestoreHint == nil {
		t.Fatalf("me = %+v, want restore_hint present", me)
	}
	hint := me.RestoreHint
	if !hint.RestoreSupported {
		t.Fatalf("restore_hint = %+v, want restore_supported", hint)
	}
	if hint.LatestSnapshot != "feedla-20260215.db" || hint.LatestSnapshotSource != "remote" {
		t.Fatalf("latest snapshot = %q from %q, want feedla-20260215.db from remote", hint.LatestSnapshot, hint.LatestSnapshotSource)
	}
}

// TestAuthRestore_TriggersCallbackWhilePending covers the happy path: with
// setup still pending and a restore callback wired in, POST /auth/restore
// invokes it and reports 202 (the server is about to restart and swap in
// the staged snapshot).
func TestAuthRestore_TriggersCallbackWhilePending(t *testing.T) {
	var calls atomic.Int64
	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{
		SetupRestore: func(context.Context) error { calls.Add(1); return nil },
	})

	resp := postJSON(t, newClientForServer(), apiSrv.URL+"/api/v1/auth/restore", map[string]string{})
	if resp.StatusCode != http.StatusAccepted {
		body, _ := decodeBody(resp)
		t.Fatalf("restore status = %d, want 202: %s", resp.StatusCode, body)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("SetupRestore called %d times, want 1", got)
	}
}

// TestAuthRestore_RejectedAfterSetup is the cross-state authorization test
// for this pre-auth endpoint: once an admin account exists, nobody --
// anonymous or otherwise -- can trigger a restore through it anymore, so a
// visitor can't overwrite a live instance's DB with an old backup.
func TestAuthRestore_RejectedAfterSetup(t *testing.T) {
	var calls atomic.Int64
	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{
		SetupRestore: func(context.Context) error { calls.Add(1); return nil },
	})
	client := newClientForServer()

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/auth/setup", map[string]string{
		"username": testUsername, "password": testPassword,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status = %d, want 200", resp.StatusCode)
	}

	// Both as the fresh admin session and as a cookie-less anonymous
	// client, restore must now be refused.
	for name, c := range map[string]*http.Client{"admin session": client, "anonymous": newClientForServer()} {
		resp := postJSON(t, c, apiSrv.URL+"/api/v1/auth/restore", map[string]string{})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("restore as %s status = %d, want 409", name, resp.StatusCode)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("SetupRestore called %d times after setup, want 0", got)
	}
}

// TestAuthRestore_UnsupportedWithoutCallback: a server without the restore
// callback (tests, embedders other than cmd/feedla serve) reports 501
// instead of pretending to restore.
func TestAuthRestore_UnsupportedWithoutCallback(t *testing.T) {
	apiSrv, _, freshClient := freshTestServer(t)

	resp := postJSON(t, freshClient, apiSrv.URL+"/api/v1/auth/restore", map[string]string{})
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("restore status = %d, want 501", resp.StatusCode)
	}
}

// TestAuthRestore_CallbackErrorIs500 keeps a failed staging (no snapshot
// found, download error) from looking like an accepted restore.
func TestAuthRestore_CallbackErrorIs500(t *testing.T) {
	apiSrv, _ := newTestServerNoLoginWithOptions(t, api.Options{
		SetupRestore: func(context.Context) error { return fmt.Errorf("no backup snapshot found") },
	})

	resp := postJSON(t, newClientForServer(), apiSrv.URL+"/api/v1/auth/restore", map[string]string{})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("restore status = %d, want 500", resp.StatusCode)
	}
}
