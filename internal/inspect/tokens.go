package inspect

import (
	"sync"
	"time"

	"github.com/tokuhirom/feedla/internal/auth"
)

// TTL is how long an issued inspect token remains redeemable. Short and
// single-use, matching §8.3/§10.3's "使い捨て・短命" requirement -- the
// legitimate flow issues a token and immediately points an iframe at it, so
// there's no reason to keep it around.
const TTL = 5 * time.Minute

// Entry is what an issued token resolves to.
type Entry struct {
	HTML     []byte
	Elements []Element
	// UserID is who issued the token. It exists purely as an optional,
	// best-effort signal for the API layer -- see Consume's doc comment --
	// not as part of TokenStore's own authorization decision.
	UserID   int64
	ExpireAt time.Time
}

// TokenStore is an in-memory, single-process registry of short-lived
// inspect view tokens. It is not backed by the database: unlike sessions
// or invitations (internal/store), these entries are meaningless past a
// few minutes and this process's lifetime, so the extra durability isn't
// worth the write traffic -- the same tradeoff internal/auth.ActionLimiter
// already makes for in-memory-only state.
type TokenStore struct {
	mu      sync.Mutex
	entries map[string]Entry
}

func NewTokenStore() *TokenStore {
	return &TokenStore{entries: make(map[string]Entry)}
}

// Issue stores html/elements under a fresh random token bound to userID and
// returns the raw token. The token itself -- 256 bits of entropy from
// auth.GenerateToken, the same source sessions and invitations use -- is
// the only thing that will later authorize reading it back (see Consume).
func (s *TokenStore) Issue(userID int64, html []byte, elements []Element, now time.Time) (string, error) {
	raw, _, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	s.entries[raw] = Entry{HTML: html, Elements: elements, UserID: userID, ExpireAt: now.Add(TTL)}
	return raw, nil
}

// Consume looks up token, deletes it unconditionally (single use, whether
// or not the lookup succeeds isn't relevant -- a token is worth at most one
// read either way), and reports whether it was present and unexpired.
//
// This deliberately does not take or check a caller identity: per
// §8.3/§10.3, the sandboxed iframe that reads the view URL back may not
// send a session cookie at all, so the token -- unguessable, single-use,
// five-minute-lived -- has to be sufficient authorization on its own. A
// caller that does have a session available may still compare it against
// the returned Entry.UserID as a best-effort extra check (see
// internal/api's handleInspectView); that check is optional by
// construction, not a gap in this method.
func (s *TokenStore) Consume(token string, now time.Time) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)

	e, ok := s.entries[token]
	delete(s.entries, token)
	if !ok || now.After(e.ExpireAt) {
		return Entry{}, false
	}
	return e, true
}

// sweepLocked drops expired entries. Called from both Issue and Consume so
// a store that's only ever issued-from (or only ever consumed-from) still
// gets cleaned up, rather than leaking every entry past its last call site.
// Called under mu.
func (s *TokenStore) sweepLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.ExpireAt) {
			delete(s.entries, k)
		}
	}
}
