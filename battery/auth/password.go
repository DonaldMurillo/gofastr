package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// RecommendedMinPasswordBytes is the minimum length application
// registration flows should enforce on new passwords. Applications use
// [ValidatePasswordStrength] to check it; [HashPassword] itself only
// rejects the empty string because hashing logic shouldn't dictate
// product policy (PIN flows, recovery tokens, machine accounts, etc.
// may legitimately fall outside this length).
const RecommendedMinPasswordBytes = 8

// ErrPasswordEmpty is returned by HashPassword when the input is the
// empty string, a blank field at signup would be silently accepted
// otherwise, and every login attempt with no password would match.
var ErrPasswordEmpty = errors.New("auth: password is empty")

// ErrPasswordTooShort is returned by ValidatePasswordStrength when the
// input is shorter than RecommendedMinPasswordBytes.
var ErrPasswordTooShort = errors.New("auth: password too short")

// PasswordHasher hashes and verifies passwords. The package ships two
// implementations: [BcryptHasher] (the default, what HashPassword has always
// used) and [Argon2Hasher] (argon2id, the modern memory-hard alternative).
//
// To change the algorithm used for NEW passwords, set [DefaultHasher] before
// auth.Init. Verification auto-detects the stored hash's format from its PHC
// prefix, so an existing user base migrates gradually: old bcrypt rows keep
// verifying, new rows use the configured hasher.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) bool
}

// DefaultHasher is used by [HashPassword] for new passwords. It is
// [BcryptHasher] by default; set it to [Argon2Hasher]{} (or a tuned
// Argon2Hasher{...}) before auth.Init to hash new registrations with argon2id.
var DefaultHasher PasswordHasher = BcryptHasher{}

// HashPassword hashes a plaintext password using [DefaultHasher] (bcrypt by
// default).
//
// The empty string is rejected with ErrPasswordEmpty. No other length
// policy is enforced, call [ValidatePasswordStrength] from the registration
// flow when you want to require a minimum length.
//
// Verification is algorithm-agnostic: [CheckPassword] detects the stored
// hash's format, so a bcrypt hash verifies whether or not DefaultHasher was
// changed.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	return DefaultHasher.Hash(password)
}

// ValidatePasswordStrength returns ErrPasswordEmpty for an empty input
// and ErrPasswordTooShort for anything shorter than
// RecommendedMinPasswordBytes. Use it from registration / password-
// change handlers to enforce a length floor without baking policy
// into the hash function.
func ValidatePasswordStrength(password string) error {
	if password == "" {
		return ErrPasswordEmpty
	}
	if len(password) < RecommendedMinPasswordBytes {
		return ErrPasswordTooShort
	}
	return nil
}

// CheckPassword compares a plaintext password against a stored hash and returns
// true if it matches. It auto-detects the algorithm from the hash's PHC prefix.
// "$argon2id$" dispatches to argon2id, anything else to bcrypt, so a single
// user table can hold a mix during a gradual migration.
//
// For bcrypt hashes, the same SHA-256 pre-hash applied in [BcryptHasher.Hash]
// is applied here for inputs longer than 72 bytes, so a long passphrase that
// was hashed at registration time still verifies at login time.
func CheckPassword(password, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return Argon2Hasher{}.Verify(password, hash)
	}
	return BcryptHasher{}.Verify(password, hash)
}

// BcryptHasher hashes with bcrypt at DefaultCost and a SHA-256 pre-hash for
// inputs longer than 72 bytes. It is the default hasher and what existing rows
// store.
type BcryptHasher struct{}

// Hash produces a bcrypt hash. Inputs longer than 72 bytes are pre-hashed with
// SHA-256 (then base64-encoded) before bcrypt: bcrypt silently truncates past
// 72 bytes, so without the pre-hash a 200-character passphrase would be
// indistinguishable from its first 72 characters, and the pre-hash avoids NUL
// bytes bcrypt would terminate on.
func (BcryptHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	input := []byte(password)
	if len(input) > 72 {
		sum := sha256.Sum256(input)
		input = []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	b, err := bcrypt.GenerateFromPassword(input, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Verify applies the same >72-byte pre-hash then compares against the bcrypt
// hash in constant time (bcrypt.CompareHashAndPassword is already
// constant-time).
func (BcryptHasher) Verify(password, hash string) bool {
	input := []byte(password)
	if len(input) > 72 {
		sum := sha256.Sum256(input)
		input = []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), input) == nil
}

// Argon2Hasher hashes with argon2id (RFC 9106), emitting a PHC-format string
// that Verify parses back:
//
//	$argon2id$v=19$m=<memory>,t=<time>,p=<threads>$<base64 salt>$<base64 key>
//
// Unlike bcrypt, argon2 accepts arbitrarily long inputs, so no pre-hash is
// needed. A zero value uses conservative OWASP-recommended parameters
// (memory 64 MiB, time 3, threads 2, 32-byte key). Tune via the exported
// fields; verify always re-derives with the parameters encoded in the stored
// hash, so a row from a differently-tuned hasher still verifies.
type Argon2Hasher struct {
	Time    uint32 // iterations.     0 → 3.
	Memory  uint32 // KiB.            0 → 65536 (64 MiB).
	Threads uint8  // parallelism.    0 → 2.
	KeyLen  uint32 // output bytes.   0 → 32.
}

// Hash produces an argon2id hash in PHC string format with a random 16-byte
// salt.
func (h Argon2Hasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	time, memory, threads, keyLen := h.params()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: argon2 salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, time, memory, threads, keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify parses the PHC string, re-derives the key with the encoded salt and
// parameters, and compares in constant time. A malformed string returns false
// (never an error), a verify call must never distinguish "bad hash" from
// "wrong password" in control flow.
func (h Argon2Hasher) Verify(password, hash string) bool {
	p, ok := parseArgon2PHC(hash)
	if !ok {
		return false
	}
	key := argon2.IDKey([]byte(password), p.salt, p.time, p.memory, p.threads, uint32(len(p.key)))
	return subtle.ConstantTimeCompare(key, p.key) == 1
}

func (h Argon2Hasher) params() (time, memory uint32, threads uint8, keyLen uint32) {
	time, memory, threads, keyLen = h.Time, h.Memory, h.Threads, h.KeyLen
	if time == 0 {
		time = 3
	}
	if memory == 0 {
		memory = 64 * 1024
	}
	if threads == 0 {
		threads = 2
	}
	if keyLen == 0 {
		keyLen = 32
	}
	return
}

// argon2PHC holds the parameters and material parsed from a PHC-format
// $argon2id$ string.
type argon2PHC struct {
	time, memory uint32
	threads      uint8
	salt, key    []byte
}

// Sane upper bounds on parameters accepted from a STORED hash. A malicious or
// corrupted hash could otherwise direct argon2.IDKey to allocate gigabytes
// (memory is KiB) or spin for unbounded CPU (time) on every verify, a per-login
// DoS. Legitimate hashes from this hasher (defaults 64 MiB / t=3 / p=2) sit far
// below these; anything larger is not a plausible password hash.
const (
	maxArgon2MemoryKiB uint32 = 1 << 20 // 1 GiB
	maxArgon2Time      uint32 = 100
	maxArgon2Threads   uint8  = 16
	maxArgon2KeyLen    uint32 = 128
)

func parseArgon2PHC(hash string) (argon2PHC, bool) {
	// strings.Split yields ["", "argon2id", "v=19", "m=M,t=T,p=P", salt, key].
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2PHC{}, false
	}
	// parts[2] is "v=19"; only argon2id v19 exists, so it is not parsed
	// further, but it must be present and well-formed.
	if !strings.HasPrefix(parts[2], "v=") {
		return argon2PHC{}, false
	}
	var p argon2PHC
	for _, field := range strings.Split(parts[3], ",") {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) != 2 {
			return argon2PHC{}, false
		}
		n, err := strconv.ParseUint(kv[1], 10, 32)
		if err != nil {
			return argon2PHC{}, false
		}
		switch kv[0] {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.time = uint32(n)
		case "p":
			p.threads = uint8(n)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2PHC{}, false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2PHC{}, false
	}
	if p.memory == 0 || p.time == 0 || p.threads == 0 || len(salt) == 0 || len(key) == 0 {
		return argon2PHC{}, false
	}
	// Reject resource-exhaustion parameters BEFORE IDKey would honour them,
	// a hostile stored hash must not allocate gigabytes or spin on verify.
	if p.memory > maxArgon2MemoryKiB || p.time > maxArgon2Time || p.threads > maxArgon2Threads || uint32(len(key)) > maxArgon2KeyLen {
		return argon2PHC{}, false
	}
	p.salt, p.key = salt, key
	return p, true
}

// dummyBcryptHash is a pre-computed bcrypt hash used to keep loginHandler
// timing-safe. When a username does not exist (or FindByEmail errors), we
// run CheckPassword against this hash so the response time is the same
// as when the user exists with a wrong password. Without this, an attacker
// can enumerate registered emails by measuring response time
// (bcrypt at default cost is ~50ms vs ~10µs for "no user").
//
// NOTE: this dummy is bcrypt-shaped. If you switch DefaultHasher to
// Argon2Hasher, real-user logins run argon2 (~tens of ms) while the
// unknown-user path still runs bcrypt against this dummy, re-aligning the
// dummy to the configured hasher's cost is a follow-up to preserve exact
// anti-enumeration timing under a full algorithm switch.
var dummyBcryptHash string

// passwordPlaceholderHash is stored as the password_hash for users created
// via OAuth or magic-link (they never log in via password). It's a real
// bcrypt hash of an unguessable random secret, recording it once at init
// avoids a per-signup ~50ms bcrypt + 64-byte allocation that previously
// happened for every first-time auto-create.
//
// Because the input is random and discarded, no password the user types
// can match. CheckPassword always returns false against this hash.
// Because the hash IS a real bcrypt structure, CheckPassword still spends
// real bcrypt time on it, preserving timing safety on the login path.
var passwordPlaceholderHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing dummy bcrypt hash: %v", err))
	}
	dummyBcryptHash = string(h)

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("auth: generating placeholder secret: %v", err))
	}
	hp, err := bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing placeholder hash: %v", err))
	}
	passwordPlaceholderHash = string(hp)
}
