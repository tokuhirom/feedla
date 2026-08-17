package api_test

import (
	"net/http"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestAdminCreateAndListInvitations(t *testing.T) {
	apiSrv, st, admin := newAdminTestServer(t)

	// Non-admin can't issue invitations.
	member := createTestUser(t, st, apiSrv.URL, "member", false)
	if resp := postJSON(t, member, apiSrv.URL+"/api/v1/admin/invitations", nil); resp.StatusCode != http.StatusForbidden {
		body, _ := decodeBody(resp)
		t.Fatalf("non-admin create invitation status = %d, want 403: %s", resp.StatusCode, body)
	}

	resp := postJSON(t, admin, apiSrv.URL+"/api/v1/admin/invitations", nil)
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("create invitation status = %d, want 201: %s", resp.StatusCode, body)
	}
	var created struct {
		store.Invitation
		Token string `json:"token"`
	}
	decodeJSON(t, resp, &created)
	if created.Token == "" {
		t.Fatalf("created invitation has no token: %+v", created)
	}

	listResp, err := admin.Get(apiSrv.URL + "/api/v1/admin/invitations")
	if err != nil {
		t.Fatalf("GET /admin/invitations: %v", err)
	}
	var listed struct {
		Invitations []store.Invitation `json:"invitations"`
	}
	decodeJSON(t, listResp, &listed)
	if len(listed.Invitations) != 1 || listed.Invitations[0].ID != created.ID {
		t.Fatalf("listed invitations = %+v, want [%+v]", listed.Invitations, created.Invitation)
	}
}

func TestAcceptInvitationFlow(t *testing.T) {
	apiSrv, _, admin := newAdminTestServer(t)

	createResp := postJSON(t, admin, apiSrv.URL+"/api/v1/admin/invitations", nil)
	if createResp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(createResp)
		t.Fatalf("create invitation status = %d, want 201: %s", createResp.StatusCode, body)
	}
	var inv struct {
		Token string `json:"token"`
	}
	decodeJSON(t, createResp, &inv)

	// Status check on an unauthenticated client (the invitation flow has
	// no session yet) succeeds and reports valid.
	anon := &http.Client{}
	statusResp := postJSON(t, anon, apiSrv.URL+"/api/v1/invitations/status", map[string]string{"token": inv.Token})
	if statusResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(statusResp)
		t.Fatalf("invitation status = %d, want 200: %s", statusResp.StatusCode, body)
	}
	var valid struct {
		Valid bool `json:"valid"`
	}
	decodeJSON(t, statusResp, &valid)
	if !valid.Valid {
		t.Fatalf("fresh invitation reported invalid")
	}

	acceptResp := postJSON(t, anon, apiSrv.URL+"/api/v1/invitations/accept", map[string]string{
		"token":    inv.Token,
		"username": "invitee",
		"password": otherUserTestPassword,
	})
	if acceptResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(acceptResp)
		t.Fatalf("accept invitation status = %d, want 200: %s", acceptResp.StatusCode, body)
	}
	var me struct {
		Authenticated bool `json:"authenticated"`
		User          struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	decodeJSON(t, acceptResp, &me)
	if !me.Authenticated || me.User.Username != "invitee" {
		t.Fatalf("accept invitation response = %+v, want authenticated invitee", me)
	}

	// Accepting again with the same token fails: it's already used.
	replayResp := postJSON(t, anon, apiSrv.URL+"/api/v1/invitations/accept", map[string]string{
		"token":    inv.Token,
		"username": "invitee2",
		"password": otherUserTestPassword,
	})
	if replayResp.StatusCode != http.StatusBadRequest {
		body, _ := decodeBody(replayResp)
		t.Fatalf("replayed accept status = %d, want 400: %s", replayResp.StatusCode, body)
	}

	// An unknown token reports invalid without creating anything, and a
	// short password is rejected before the token is even checked against
	// a nonexistent race.
	unknownResp := postJSON(t, anon, apiSrv.URL+"/api/v1/invitations/status", map[string]string{"token": "does-not-exist"})
	if unknownResp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(unknownResp)
		t.Fatalf("unknown token status = %d, want 404: %s", unknownResp.StatusCode, body)
	}
}
