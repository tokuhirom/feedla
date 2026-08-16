// Package auth implements feedla's authentication primitives: argon2id
// password hashing, session/API token generation, and login rate limiting.
// See docs/multi-user-design.md for the design this implements (Phase A).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters per docs/multi-user-design.md's "パスワードハッシュ"
// section: 64 MiB memory, 3 iterations, parallelism 2. Login is infrequent
// enough that this cost is acceptable; loginSem below bounds concurrent
// hashing so several simultaneous login attempts can't spike RSS.
const (
	argonMemory  uint32 = 64 * 1024 // KiB
	argonTime    uint32 = 3
	argonThreads uint8  = 2
	argonSaltLen        = 16
	argonKeyLen  uint32 = 32
)

// loginSem bounds concurrent argon2id hashing (both on login verification
// and on hash creation) to 2 at a time, per the design doc's guidance to
// serialize login processing so concurrent attempts don't spike memory use.
var loginSem = make(chan struct{}, 2)

func withLoginSem(fn func()) {
	loginSem <- struct{}{}
	defer func() { <-loginSem }()
	fn()
}

// ErrInvalidHash is returned by VerifyPassword when the stored hash isn't a
// well-formed argon2id PHC string (e.g. the '!locked!' sentinel used for
// users who haven't completed setup yet).
var ErrInvalidHash = errors.New("auth: invalid password hash")

// HashPassword hashes plain with argon2id and returns a PHC-formatted
// string ($argon2id$v=19$m=...,t=...,p=...$salt$hash) suitable for storage
// in users.password_hash.
func HashPassword(plain string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	var hash []byte
	withLoginSem(func() {
		hash = argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	})

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64Encode(salt), b64Encode(hash)), nil
}

// VerifyPassword reports whether plain matches hash, a PHC string produced
// by HashPassword. It always performs a constant-time comparison of the
// derived key. If hash isn't a parseable argon2id PHC string (e.g. the
// '!locked!' sentinel), it returns ErrInvalidHash and ok=false without
// hashing anything -- callers that need uniform timing regardless of
// whether the hash is well-formed should call VerifyDummyPassword instead
// (or in addition, for the "user doesn't exist" case).
func VerifyPassword(hash, plain string) (ok bool, err error) {
	params, salt, want, err := parsePHC(hash)
	if err != nil {
		return false, err
	}

	var got []byte
	withLoginSem(func() {
		got = argon2.IDKey([]byte(plain), salt, params.time, params.memory, params.threads, uint32(len(want)))
	})

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is a fixed, valid argon2id PHC hash used by VerifyDummyPassword
// to burn roughly the same CPU time as a real verification when the
// username doesn't exist, so login responses don't leak which usernames
// are registered via timing.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// VerifyDummyPassword performs a throwaway argon2id verification with the
// same cost parameters as a real login, so that "user does not exist" and
// "wrong password" take the same amount of time from the caller's
// perspective.
func VerifyDummyPassword(plain string) {
	_, _ = VerifyPassword(dummyHash, plain)
}

type phcParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parsePHC(hash string) (phcParams, []byte, []byte, error) {
	// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return phcParams{}, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return phcParams{}, nil, nil, ErrInvalidHash
	}

	var p phcParams
	var m, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &threads); err != nil {
		return phcParams{}, nil, nil, ErrInvalidHash
	}
	p.memory, p.time, p.threads = m, t, threads

	salt, err := b64Decode(parts[4])
	if err != nil {
		return phcParams{}, nil, nil, ErrInvalidHash
	}
	key, err := b64Decode(parts[5])
	if err != nil {
		return phcParams{}, nil, nil, ErrInvalidHash
	}

	return p, salt, key, nil
}

// NeedsRehash reports whether hash was produced with different parameters
// than the current defaults, so callers can transparently re-hash on
// successful login (per docs/multi-user-design.md).
func NeedsRehash(hash string) bool {
	p, _, _, err := parsePHC(hash)
	if err != nil {
		return false
	}
	return p.memory != argonMemory || p.time != argonTime || p.threads != argonThreads
}
