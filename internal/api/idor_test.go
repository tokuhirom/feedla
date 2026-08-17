package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tokuhirom/feedla/internal/api"
	"github.com/tokuhirom/feedla/internal/store"
)

// TestIDOR* cover docs/multi-user-design.md's §認可 completion condition for
// Phase C ("2 ユーザーでの e2e: 相互に見えない・操作できないこと"): every
// store-layer function already takes userID and scopes its WHERE clause on
// it (see [[project-feedla-overview]]'s Phase B notes), but until now only
// scrape_sources and stats had a regression test proving a second user
// actually gets rejected. These fill in the remaining resources:
// subscriptions (PATCH/DELETE/folder_id), pins, ignore_words, bulk
// entries/read, and the LDR-compatible endpoints.

// TestIDORSubscriptionPatchAndDeleteOwnershipEnforced covers PATCH/DELETE
// /api/v1/subscriptions/{id}: a user who never subscribed to feedID must
// get 404 (not touch someone else's subscription), while the owner keeps
// working normally.
func TestIDORSubscriptionPatchAndDeleteOwnershipEnforced(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	feedID := created.Subscription.FeedID

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	patchURL := fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, feedID)
	if resp := patchJSON(t, other, patchURL, map[string]any{"rating": 5}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner PATCH status = %d, want 404: %s", resp.StatusCode, body)
	}
	if resp := deleteReq(t, other, patchURL); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner DELETE status = %d, want 404: %s", resp.StatusCode, body)
	}

	// The owner's subscription must be untouched by the rejected attempts.
	listResp, err := owner.Get(apiSrv.URL + "/api/v1/subscriptions")
	if err != nil {
		t.Fatalf("GET subscriptions: %v", err)
	}
	var list struct {
		Subscriptions []store.SubscriptionView `json:"subscriptions"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.Subscriptions) != 1 || list.Subscriptions[0].Rating != 0 {
		t.Fatalf("owner subscriptions = %+v, want one untouched (rating 0) subscription", list.Subscriptions)
	}

	if resp := patchJSON(t, owner, patchURL, map[string]any{"rating": 5}); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("owner PATCH status = %d, want 200: %s", resp.StatusCode, body)
	}
}

// TestIDORSubscriptionFolderIDCrossUserRejected covers the "folder_id
// specified on a PATCH must be one of the caller's own folders" check
// (subscriptions.go's handlePatchSubscription): another user's folder id
// must be rejected even though the caller does own a subscription to
// patch.
func TestIDORSubscriptionFolderIDCrossUserRejected(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/folders", map[string]string{"name": "Owner's Folder"})
	var folder store.Folder
	decodeJSON(t, resp, &folder)

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)
	resp = postJSON(t, other, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	otherFeedID := created.Subscription.FeedID

	patchURL := fmt.Sprintf("%s/api/v1/subscriptions/%d", apiSrv.URL, otherFeedID)
	if resp := patchJSON(t, other, patchURL, map[string]any{"folder_id": folder.ID}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("cross-user folder_id PATCH status = %d, want 404: %s", resp.StatusCode, body)
	}
}

// TestIDORPinAddAndRemoveOwnershipEnforced covers POST /api/v1/pins and
// DELETE /api/v1/pins/{id}: pinning requires the caller to actually
// subscribe to the entry's feed (store.AddPin checks user_entry_state),
// and removing a pin only ever touches the caller's own pins row.
func TestIDORPinAddAndRemoveOwnershipEnforced(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)

	entriesResp, err := owner.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrv.URL, created.Subscription.FeedID))
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var entriesList struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, entriesResp, &entriesList)
	if len(entriesList.Entries) == 0 {
		t.Fatal("owner has no entries to pin")
	}
	entryID := entriesList.Entries[0].ID

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	// other never subscribed to this feed, so pinning owner's entry must
	// be rejected -- otherwise a non-subscriber could probe entry ids.
	if resp := postJSON(t, other, apiSrv.URL+"/api/v1/pins", map[string]any{"entry_id": entryID}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-subscriber pin add status = %d, want 404: %s", resp.StatusCode, body)
	}

	// The owner pins it for real.
	if resp := postJSON(t, owner, apiSrv.URL+"/api/v1/pins", map[string]any{"entry_id": entryID}); resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("owner pin add status = %d, want 201: %s", resp.StatusCode, body)
	}

	// other still has no pin row for this entry, so removing it 404s --
	// it must not be able to un-pin something the owner pinned.
	pinURL := fmt.Sprintf("%s/api/v1/pins/%d", apiSrv.URL, entryID)
	if resp := deleteReq(t, other, pinURL); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner pin remove status = %d, want 404: %s", resp.StatusCode, body)
	}

	pinsResp, err := owner.Get(apiSrv.URL + "/api/v1/pins")
	if err != nil {
		t.Fatalf("GET pins: %v", err)
	}
	var pinsList struct {
		Pins []store.Pin `json:"pins"`
	}
	decodeJSON(t, pinsResp, &pinsList)
	if len(pinsList.Pins) != 1 {
		t.Fatalf("owner pins = %+v, want the pin to still be there", pinsList.Pins)
	}
}

// TestIDORIgnoreWordRemoveOwnershipEnforced covers DELETE
// /api/v1/ignore_words/{id}.
func TestIDORIgnoreWordRemoveOwnershipEnforced(t *testing.T) {
	apiSrv, _, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/ignore_words", map[string]string{"word": "spoiler"})
	if resp.StatusCode != http.StatusCreated {
		body, _ := decodeBody(resp)
		t.Fatalf("add ignore word status = %d, want 201: %s", resp.StatusCode, body)
	}

	listResp, err := owner.Get(apiSrv.URL + "/api/v1/ignore_words")
	if err != nil {
		t.Fatalf("GET ignore_words: %v", err)
	}
	var list struct {
		IgnoreWords []store.IgnoreWord `json:"ignore_words"`
	}
	decodeJSON(t, listResp, &list)
	if len(list.IgnoreWords) != 1 {
		t.Fatalf("ignore_words = %+v, want one", list.IgnoreWords)
	}
	wordID := list.IgnoreWords[0].ID

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)
	wordURL := fmt.Sprintf("%s/api/v1/ignore_words/%d", apiSrv.URL, wordID)
	if resp := deleteReq(t, other, wordURL); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-owner DELETE status = %d, want 404: %s", resp.StatusCode, body)
	}

	listResp, err = owner.Get(apiSrv.URL + "/api/v1/ignore_words")
	if err != nil {
		t.Fatalf("GET ignore_words after rejected delete: %v", err)
	}
	decodeJSON(t, listResp, &list)
	if len(list.IgnoreWords) != 1 {
		t.Fatalf("ignore_words after rejected delete = %+v, want it to still be there", list.IgnoreWords)
	}

	if resp := deleteReq(t, owner, wordURL); resp.StatusCode != http.StatusNoContent {
		body, _ := decodeBody(resp)
		t.Fatalf("owner DELETE status = %d, want 204: %s", resp.StatusCode, body)
	}
}

// TestIDORBulkMarkReadNotAnOracle covers POST /api/v1/entries/read: mixing
// another user's entry ids into the bulk list must not error (that would
// let a caller learn which entry ids exist system-wide by bisecting on
// status code) -- the store just marks whichever of the caller's own
// unread rows match, silently ignoring the rest (see
// docs/multi-user-design.md's bulk-operation guidance).
func TestIDORBulkMarkReadNotAnOracle(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var ownerSub struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &ownerSub)
	ownerEntries := listEntries(t, owner, apiSrv.URL, ownerSub.Subscription.FeedID)
	if len(ownerEntries) == 0 {
		t.Fatal("owner has no entries")
	}
	ownerEntryID := ownerEntries[0].ID

	// A second, distinct feed so other has their own real entry to mix in
	// with the probe.
	feedSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, strings.ReplaceAll(testFeedXML, "example.com", "example.net"))
	}))
	t.Cleanup(feedSrv2.Close)

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)
	resp = postJSON(t, other, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv2.URL})
	var otherSub struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &otherSub)
	otherEntries := listEntries(t, other, apiSrv.URL, otherSub.Subscription.FeedID)
	if len(otherEntries) == 0 {
		t.Fatal("other has no entries")
	}
	otherEntryID := otherEntries[0].ID

	markResp := postJSON(t, other, apiSrv.URL+"/api/v1/entries/read", map[string]any{
		"entry_ids": []int64{ownerEntryID, otherEntryID},
	})
	if markResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(markResp)
		t.Fatalf("bulk mark-read status = %d, want 200: %s", markResp.StatusCode, body)
	}
	var markBody struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, markResp, &markBody)
	if markBody.MarkedRead != 1 {
		t.Fatalf("marked_read = %d, want 1 (only other's own entry, owner's silently ignored)", markBody.MarkedRead)
	}

	// The owner's entry must still be unread.
	unreadResp, err := owner.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries?unread=1", apiSrv.URL, ownerSub.Subscription.FeedID))
	if err != nil {
		t.Fatalf("GET owner unread entries: %v", err)
	}
	var unreadList struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, unreadResp, &unreadList)
	if len(unreadList.Entries) != len(ownerEntries) {
		t.Fatalf("owner unread entries = %d, want %d (unchanged by other's bulk mark-read)", len(unreadList.Entries), len(ownerEntries))
	}
}

func listEntries(t *testing.T, client *http.Client, apiSrvURL string, feedID int64) []store.Entry {
	t.Helper()
	resp, err := client.Get(fmt.Sprintf("%s/api/v1/subscriptions/%d/entries", apiSrvURL, feedID))
	if err != nil {
		t.Fatalf("GET entries: %v", err)
	}
	var list struct {
		Entries []store.Entry `json:"entries"`
	}
	decodeJSON(t, resp, &list)
	return list.Entries
}

// TestIDORLDRCompatSubscribeIDCrossUserNotOracle covers the Fastladder-
// compatible endpoints (internal/api/ldr.go), which key off subscribe_id
// (== feed_id) supplied in the form body rather than a path parameter.
// Read-only/idempotent operations (unread, touch_all) on a feed the caller
// doesn't subscribe to must behave exactly like "nothing to do" (200,
// empty/zero) rather than erroring -- an error response would let a caller
// distinguish "this feed_id doesn't exist" from "it exists but isn't
// mine". unsubscribe, being a real mutation with no query-then-act split,
// does 404 like its v1 counterpart, but that 404 carries no more
// information than "you don't subscribe to this feed_id", true whether or
// not the feed_id exists or belongs to someone else.
func TestIDORLDRCompatSubscribeIDCrossUserNotOracle(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postForm(t, owner, apiSrv.URL+"/api/subscribe", url.Values{"feedlink": {feedSrv.URL}})
	var subResp struct {
		SubscribeID int64 `json:"subscribe_id"`
	}
	decodeJSON(t, resp, &subResp)

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	unreadResp := postForm(t, other, apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	if unreadResp.StatusCode != http.StatusOK {
		body, _ := decodeBody(unreadResp)
		t.Fatalf("non-subscriber /api/unread status = %d, want 200: %s", unreadResp.StatusCode, body)
	}
	var entries []store.Entry
	decodeJSON(t, unreadResp, &entries)
	if len(entries) != 0 {
		t.Fatalf("non-subscriber /api/unread entries = %+v, want empty (owner's entries not leaked)", entries)
	}

	touchResp := postForm(t, other, apiSrv.URL+"/api/touch_all", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	var touchBody struct {
		MarkedRead int `json:"marked_read"`
	}
	decodeJSON(t, touchResp, &touchBody)
	if touchBody.MarkedRead != 0 {
		t.Fatalf("non-subscriber touch_all marked_read = %d, want 0 (must not mark owner's entries read)", touchBody.MarkedRead)
	}

	if resp := postForm(t, other, apiSrv.URL+"/api/unsubscribe", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-subscriber unsubscribe status = %d, want 404: %s", resp.StatusCode, body)
	}

	// The owner's subscription and unread entries must be completely
	// unaffected by other's attempts.
	ownerUnreadResp := postForm(t, owner, apiSrv.URL+"/api/unread", url.Values{"subscribe_id": {fmt.Sprint(subResp.SubscribeID)}})
	decodeJSON(t, ownerUnreadResp, &entries)
	if len(entries) != 2 {
		t.Fatalf("owner /api/unread entries = %d, want 2 (untouched)", len(entries))
	}
}

// TestIDORLDRCompatPinByLinkCrossUserRejected covers /api/pin/add and
// /api/pin/remove, which resolve the pin target by URL (FindEntryByURL is
// global, unscoped by user -- see internal/store/pins.go) and rely on
// AddPin/RemovePin's own userID checks for authorization.
func TestIDORLDRCompatPinByLinkCrossUserRejected(t *testing.T) {
	apiSrv, feedSrv, owner, st := newTestServerWithStoreAndOptions(t, api.Options{})

	resp := postJSON(t, owner, apiSrv.URL+"/api/v1/subscriptions", map[string]string{"url": feedSrv.URL})
	var created struct {
		Subscription *store.SubscriptionView `json:"subscription"`
	}
	decodeJSON(t, resp, &created)
	entries := listEntries(t, owner, apiSrv.URL, created.Subscription.FeedID)
	if len(entries) == 0 {
		t.Fatal("owner has no entries")
	}
	link := entries[0].URL

	other := createTestUser(t, st, apiSrv.URL, "other-user", false)

	// other doesn't subscribe to this feed, so pinning by link must fail
	// exactly like pinning by id does.
	if resp := postForm(t, other, apiSrv.URL+"/api/pin/add", url.Values{"link": {link}}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-subscriber pin/add status = %d, want 404: %s", resp.StatusCode, body)
	}

	if resp := postForm(t, owner, apiSrv.URL+"/api/pin/add", url.Values{"link": {link}}); resp.StatusCode != http.StatusOK {
		body, _ := decodeBody(resp)
		t.Fatalf("owner pin/add status = %d, want 200: %s", resp.StatusCode, body)
	}

	// other still isn't a subscriber, so it can't remove the owner's pin
	// either.
	if resp := postForm(t, other, apiSrv.URL+"/api/pin/remove", url.Values{"link": {link}}); resp.StatusCode != http.StatusNotFound {
		body, _ := decodeBody(resp)
		t.Fatalf("non-subscriber pin/remove status = %d, want 404: %s", resp.StatusCode, body)
	}

	pinsResp := postForm(t, owner, apiSrv.URL+"/api/pin/all", url.Values{})
	var pins []store.Pin
	decodeJSON(t, pinsResp, &pins)
	if len(pins) != 1 {
		t.Fatalf("owner pins = %+v, want the pin to still be there", pins)
	}
}
