package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Regression tests from the 2026-11-01 battery/auth bug bash: the
// password hashing surface.

// fastArgon2 is an argon2id hasher cheap enough for the suite.
var fastArgon2 = Argon2Hasher{Time: 1, Memory: 8 * 1024, Threads: 1}

// withDefaultHasher swaps DefaultHasher for one test.
func withDefaultHasher(t *testing.T, h PasswordHasher) {
	t.Helper()
	orig := DefaultHasher
	DefaultHasher = h
	t.Cleanup(func() { DefaultHasher = orig })
}

// Property: every passwordless account's stored hash is made by the
// configured hasher. The login path verifies a stored hash with its own
// algorithm and burns an unknown email's dummy with DefaultHasher's, so
// a bcrypt placeholder under DefaultHasher = Argon2Hasher answered in
// bcrypt time while an unknown email answered in argon2 time: the gap
// told an attacker which addresses hold an OAuth / magic-link account.
// Pinned on the PHC marker, not the clock.
func TestPasswordlessPlaceholderTracksDefaultHasher(t *testing.T) {
	withDefaultHasher(t, fastArgon2)

	if got := placeholderPasswordHash(); !strings.HasPrefix(got, "$argon2id$") {
		t.Errorf("shared placeholder under argon2 default = %q, want an $argon2id$ hash", phcAlgoPrefix(got))
	}
	fresh, err := newPlaceholderHash()
	if err != nil {
		t.Fatalf("newPlaceholderHash: %v", err)
	}
	if !strings.HasPrefix(fresh, "$argon2id$") {
		t.Errorf("cleared-password placeholder under argon2 default = %q, want an $argon2id$ hash", phcAlgoPrefix(fresh))
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	users := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	u, err := users.CreateUserNoPassword(ctx, "oauth@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUserNoPassword: %v", err)
	}
	_, stored, err := users.FindByEmail(ctx, "oauth@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if !strings.HasPrefix(stored, "$argon2id$") {
		t.Errorf("SECURITY: [timing] CreateUserNoPassword stored a %q placeholder while unknown emails burn an argon2id dummy: the verify-time gap enumerates passwordless accounts", phcAlgoPrefix(stored))
	}
	if CheckPassword("anything", stored) {
		t.Fatal("placeholder verified a guessed password")
	}
	if err := users.ClearPassword(ctx, u.GetID()); err != nil {
		t.Fatalf("ClearPassword: %v", err)
	}
	_, cleared, _ := users.FindByEmail(ctx, "oauth@example.com")
	if !strings.HasPrefix(cleared, "$argon2id$") {
		t.Errorf("ClearPassword stored a %q placeholder under the argon2 default", phcAlgoPrefix(cleared))
	}
	if cleared == stored {
		t.Error("ClearPassword wrote the shared placeholder back; the credential tag would not move")
	}
}

// pepperHasher is a custom PasswordHasher shaped like the ones hosts
// write: bcrypt over a server-side pepper. Its hashes are bcrypt-shaped,
// so a dispatcher keyed on the PHC prefix alone sends them to bare
// bcrypt, which never matches.
type pepperHasher struct {
	pepper   string
	verifies *atomic.Int64
}

func (h pepperHasher) Hash(pw string) (string, error) { return BcryptHasher{}.Hash(h.pepper + pw) }
func (h pepperHasher) Verify(pw, hash string) bool {
	if h.verifies != nil {
		h.verifies.Add(1)
	}
	return BcryptHasher{}.Verify(h.pepper+pw, hash)
}

// shaHasher is a custom hasher with a prefix neither built-in knows.
type shaHasher struct{}

func (shaHasher) Hash(pw string) (string, error) {
	sum := sha256.Sum256([]byte("salt:" + pw))
	return "$test-sha$" + hex.EncodeToString(sum[:]), nil
}
func (h shaHasher) Verify(pw, hash string) bool {
	want, _ := h.Hash(pw)
	return want == hash
}

// Property: a host's DefaultHasher verifies the hashes it made. The
// PHC-prefix dispatch routed everything that was not $argon2id$ to
// bcrypt, so a custom hasher's Verify was never called and every login
// failed, and the not-found dummy it produced was rejected by bcrypt's
// format check in microseconds.
func TestVerifyPasswordConsultsCustomDefaultHasher(t *testing.T) {
	var calls atomic.Int64
	for _, h := range []PasswordHasher{pepperHasher{pepper: "p3pp3r", verifies: &calls}, shaHasher{}} {
		withDefaultHasher(t, h)
		hash, err := HashPassword("correct horse")
		if err != nil {
			t.Fatalf("%T hash: %v", h, err)
		}
		if !verifyPassword("correct horse", hash) {
			t.Errorf("%T: verifyPassword refused the password DefaultHasher just hashed", h)
		}
		if verifyPassword("wrong horse", hash) {
			t.Errorf("%T: verifyPassword accepted a wrong password", h)
		}
	}

	// The dummy burned for an unknown email goes through the custom
	// Verify, so it costs what a real row costs.
	withDefaultHasher(t, pepperHasher{pepper: "p3pp3r", verifies: &calls})
	before := calls.Load()
	_ = verifyPassword("guess", dummyHashFor(DefaultHasher))
	if calls.Load() == before {
		t.Error("SECURITY: [timing] the not-found dummy never reached the custom hasher's Verify")
	}

	// Rows made before the switch keep verifying.
	old, _ := BcryptHasher{}.Hash("legacy-pass")
	if !verifyPassword("legacy-pass", old) {
		t.Error("a bcrypt row stopped verifying under a custom DefaultHasher")
	}
	a2, _ := fastArgon2.Hash("argon-pass")
	if !verifyPassword("argon-pass", a2) {
		t.Error("an argon2id row stopped verifying under a custom DefaultHasher")
	}
	// And a peppered hash never verifies without the pepper.
	withDefaultHasher(t, BcryptHasher{})
	peppered, _ := pepperHasher{pepper: "p3pp3r"}.Hash("pw-with-pepper")
	if verifyPassword("pw-with-pepper", peppered) {
		t.Error("a peppered hash verified under bare bcrypt")
	}
}

// Property: Argon2Hasher.Hash refuses parameters its own Verify refuses.
// The verifier caps a stored hash's cost (a hostile row must not
// allocate gigabytes), so a hasher tuned past a cap minted hashes no
// login could ever verify: every account registered under it was locked
// out with no error anywhere.
func TestArgon2HashRefusesParamsVerifyRefuses(t *testing.T) {
	for name, h := range map[string]Argon2Hasher{
		"threads": {Time: 1, Memory: 8 * 1024, Threads: maxArgon2Threads + 1},
		"time":    {Time: maxArgon2Time + 1, Memory: 8 * 1024, Threads: 1},
		"keylen":  {Time: 1, Memory: 8 * 1024, Threads: 1, KeyLen: maxArgon2KeyLen + 1},
		"memory":  {Time: 1, Memory: maxArgon2MemoryKiB + 1, Threads: 1},
	} {
		hash, err := h.Hash("pw-123456")
		if err == nil {
			t.Errorf("%s: Hash(%+v) = %q, nil; Verify refuses these params, so the hash can never sign in", name, h, phcAlgoPrefix(hash))
			if h.Verify("pw-123456", hash) {
				t.Errorf("%s: and yet it verified", name)
			}
		}
	}
	// At the caps it still works.
	h := Argon2Hasher{Time: 1, Memory: 8 * 1024, Threads: maxArgon2Threads, KeyLen: maxArgon2KeyLen}
	hash, err := h.Hash("pw-123456")
	if err != nil || !h.Verify("pw-123456", hash) {
		t.Fatalf("hash at the caps: err=%v verify=%v", err, h.Verify("pw-123456", hash))
	}
}

// Property: the length floor counts characters, as its message says.
// It counted bytes, so two emoji (8 bytes) passed "at least 8
// characters" and a seven-letter accented password was let through on
// its byte count.
func TestPasswordFloorCountsCharacters(t *testing.T) {
	for _, short := range []string{"🔑🔑", "ñandú12", "пароль1"} {
		if err := ValidatePasswordStrength(short); !errors.Is(err, ErrPasswordTooShort) {
			t.Errorf("ValidatePasswordStrength(%q) = %v; %d characters is under the floor", short, err, len([]rune(short)))
		}
	}
	for _, ok := range []string{"ñandúes1", "hunter22", "🔑🔑🔑🔑🔑🔑🔑🔑"} {
		if err := ValidatePasswordStrength(ok); err != nil {
			t.Errorf("ValidatePasswordStrength(%q) = %v; 8 characters meets the floor", ok, err)
		}
	}
}

// countingHasher has a mutable field: keyed on its field values, every
// call was a new cache key, paying a full KDF and growing the dummy and
// placeholder caches without bound (2026-11-01 review).
type countingHasher struct{ calls *int }

func (h countingHasher) Hash(p string) (string, error) {
	*h.calls++
	return BcryptHasher{}.Hash(p)
}
func (h countingHasher) Verify(p, hash string) bool { return BcryptHasher{}.Verify(p, hash) }

func TestCustomHasherDummyCachedOnce(t *testing.T) {
	withDefaultBcryptCost(t)
	n := 0
	h := countingHasher{calls: &n}
	a := dummyHashFor(h)
	b := dummyHashFor(countingHasher{calls: new(int)})
	if a != b || n != 1 {
		t.Errorf("custom hasher dummy derived %d times (same=%v), want once per type", n, a == b)
	}
}

// recursingHasher's Verify calls CheckPassword, the natural spelling of a
// custom hasher written against "verification auto-detects the format".
// With the custom hasher asked from inside CheckPassword, that recursed
// until the stack overflowed: any anonymous login killed the process
// (2026-11-01 review, round 2).
type recursingHasher struct{ depth *atomic.Int64 }

func (h recursingHasher) Hash(p string) (string, error) { return BcryptHasher{}.Hash(p) }
func (h recursingHasher) Verify(p, hash string) bool {
	if h.depth.Add(1) > 3 {
		return false // the guard a real hasher would not have
	}
	return CheckPassword(p, hash)
}

func TestCustomVerifyCallingCheckPasswordDoesNotRecurse(t *testing.T) {
	withDefaultBcryptCost(t)
	var depth atomic.Int64
	withDefaultHasher(t, recursingHasher{depth: &depth})
	hash, _ := HashPassword("pw-12345678")
	if !verifyPassword("pw-12345678", hash) {
		t.Error("the right password was refused")
	}
	if got := depth.Load(); got != 1 {
		t.Errorf("SECURITY: the custom Verify was re-entered %d times through CheckPassword, want 1", got)
	}
}
