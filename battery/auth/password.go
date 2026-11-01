package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// RecommendedMinPasswordBytes is the minimum length, in characters
// (Unicode code points, not bytes, whatever the name says), that
// application registration flows should enforce on new passwords.
// Applications use
// [ValidatePasswordStrength] to check it; [HashPassword] itself only
// rejects the empty string because hashing logic shouldn't dictate
// product policy (PIN flows, recovery tokens, machine accounts, etc.
// may legitimately fall outside this length).
const RecommendedMinPasswordBytes = 8

// ErrPasswordEmpty is returned by HashPassword when the input is the
// empty string, a blank field at signup would be silently accepted
// otherwise, and every login attempt with no password would match.
var ErrPasswordEmpty error = i18n.NewError(reasonPasswordEmpty)

// ErrPasswordTooShort is returned by ValidatePasswordStrength when the
// input has fewer than RecommendedMinPasswordBytes characters.
var ErrPasswordTooShort error = i18n.NewError(reasonPasswordTooShortField)

// PasswordHasher hashes and verifies passwords. The package ships two
// implementations: [BcryptHasher] (the default, what HashPassword has always
// used) and [Argon2Hasher] (argon2id, the modern memory-hard alternative).
//
// To change the algorithm used for NEW passwords, set [DefaultHasher] before
// auth.Init. Verification auto-detects the stored hash's format from its PHC
// prefix, so an existing user base migrates gradually: old bcrypt rows keep
// verifying, new rows use the configured hasher. A custom hasher (a
// pepper, scrypt) works too: the battery's sign-in and password-change
// paths ask DefaultHasher.Verify first whenever it is not one of the two
// built-ins, then fall back to [CheckPassword], which knows only the
// built-in formats (so a custom Verify may call it). Passwordless
// accounts (OAuth, magic link) store a placeholder made by the
// configured hasher, so their verify costs what any other row's does.
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
// and ErrPasswordTooShort for anything with fewer than
// RecommendedMinPasswordBytes characters. It counts characters (code
// points), as its message says, not bytes: "🔑🔑" is two characters.
// Use it from registration / password-change handlers to enforce a
// length floor without baking policy into the hash function.
func ValidatePasswordStrength(password string) error {
	if password == "" {
		return ErrPasswordEmpty
	}
	if utf8.RuneCountInString(password) < RecommendedMinPasswordBytes {
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
//
// CheckPassword knows the two built-in formats only, so a custom
// [PasswordHasher]'s Verify may call it for rows written before the switch.
// The battery's own sign-in paths also ask a custom [DefaultHasher] first
// (see verifyPassword).
func CheckPassword(password, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return Argon2Hasher{}.Verify(password, hash)
	}
	return BcryptHasher{}.Verify(password, hash)
}

// verifyPassword is the battery's own password check: login, password
// change, and the unknown-account dummy. When [DefaultHasher] is a custom
// hasher (neither [BcryptHasher] nor [Argon2Hasher]), its Verify is asked
// first: a peppered bcrypt hash looks like any other bcrypt hash, so the
// prefix cannot route it. A hash it does not accept goes through
// [CheckPassword], so rows written before the switch keep verifying.
//
// It is not CheckPassword itself: a custom Verify written against the
// documented contract ("verification auto-detects the format") calls
// CheckPassword, and asking the custom hasher from inside CheckPassword
// recursed until the stack overflowed, killing the process on any
// anonymous login attempt.
func verifyPassword(password, hash string) bool {
	if isCustomHasher(DefaultHasher) && DefaultHasher.Verify(password, hash) {
		return true
	}
	return CheckPassword(password, hash)
}

// isCustomHasher reports whether h is a host-supplied hasher rather than
// one of the two built-ins (by value or by pointer).
func isCustomHasher(h PasswordHasher) bool {
	switch h.(type) {
	case nil, BcryptHasher, *BcryptHasher, Argon2Hasher, *Argon2Hasher:
		return false
	}
	return true
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
	b, err := bcrypt.GenerateFromPassword(input, bcryptCost)
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
//
// Parameters past the caps [Argon2Hasher.Verify] enforces on a stored hash
// (1 GiB of memory, 100 iterations, 16 threads, a 128-byte key) are refused
// with an error: no hash made with them could ever verify, so every account
// created under them would be locked out.
func (h Argon2Hasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	time, memory, threads, keyLen := h.params()
	if memory > maxArgon2MemoryKiB || time > maxArgon2Time || threads > maxArgon2Threads || keyLen > maxArgon2KeyLen {
		return "", fmt.Errorf("auth: argon2 parameters m=%d,t=%d,p=%d,keylen=%d exceed what Verify accepts (m<=%d, t<=%d, p<=%d, keylen<=%d), so no hash made with them could sign in",
			memory, time, threads, keyLen, maxArgon2MemoryKiB, maxArgon2Time, maxArgon2Threads, maxArgon2KeyLen)
	}
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
// Argon2Hasher.Hash refuses parameters past them, so the hasher never makes
// a hash its own verifier refuses.
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
	for field := range strings.SplitSeq(parts[3], ",") {
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

// bcryptCost is the work factor every bcrypt hash in this package uses:
// passwords, backup codes and the timing dummies below. A var only so
// the package's tests can hash at bcrypt.MinCost; nothing else assigns
// it.
var bcryptCost = bcrypt.DefaultCost

// dummyBcryptHash is a pre-computed bcrypt hash used to keep loginHandler
// timing-safe. When a username does not exist (or FindByEmail errors), we
// run CheckPassword against this hash so the response time is the same
// as when the user exists with a wrong password. Without this, an attacker
// can enumerate registered emails by measuring response time
// (bcrypt at default cost is ~50ms vs ~10µs for "no user").
//
// The dummy must be shaped by DefaultHasher, not pinned to bcrypt. Under
// DefaultHasher = Argon2Hasher a real login spends argon2 time while the
// not-found branch spent bcrypt time, and the gap between the two
// algorithms is itself the oracle the dummy exists to close (CWE-208).
// [dummyHashFor] derives one per configured hasher instead.
var dummyBcryptHash string

// dummyHashes caches one dummy per hasher configuration (hasherKey).
// Deriving it costs a full KDF run, so doing it per failed login would hand
// an attacker a cheap way to make the server work harder than a real login
// does.
var dummyHashes sync.Map

// hasherKey names a hasher configuration. The built-ins are keyed by type
// and field values, so two differently tuned Argon2Hashers each get a
// cached hash at their own cost. A custom hasher is keyed by its type
// only: its fields may be mutable (a counter, a pointer) or secret (a
// pepper), and keying on them made every call a new key, paying a full
// KDF run and growing the caches without bound.
func hasherKey(h PasswordHasher) string {
	switch h.(type) {
	case BcryptHasher, *BcryptHasher, Argon2Hasher, *Argon2Hasher:
		return fmt.Sprintf("%T|%+v", h, h)
	}
	return fmt.Sprintf("%T", h)
}

// dummyHashFor returns a dummy hash produced by the CONFIGURED hasher, so
// the not-found branch spends the same algorithm and cost a real row does.
// A hasher that cannot produce one falls back to the bcrypt dummy: wrong
// timing beats no timing defence at all.
func dummyHashFor(h PasswordHasher) string {
	if h == nil {
		return dummyBcryptHash
	}
	key := hasherKey(h)
	if v, ok := dummyHashes.Load(key); ok {
		return v.(string)
	}
	hash, err := h.Hash("dummy-password-for-timing")
	if err != nil {
		return dummyBcryptHash
	}
	dummyHashes.Store(key, hash)
	return hash
}

// passwordPlaceholderHash is the bcrypt placeholder made at init: a real
// bcrypt hash of an unguessable random secret. It is what
// [placeholderPasswordHash] answers under the default BcryptHasher, and its
// fallback when the configured hasher cannot hash.
//
// Because the input is random and discarded, no password the user types
// can match. CheckPassword always returns false against this hash.
var passwordPlaceholderHash string

// placeholderHashes caches one shared placeholder per hasher configuration
// (hasherKey), so an auto-create does not pay a full KDF run per signup.
var placeholderHashes sync.Map

// placeholderPasswordHash is the password_hash stored for users created
// via OAuth or magic link (they never log in via password): a hash of an
// unguessable random secret, made by the CONFIGURED hasher.
//
// The hasher matters. A login verifies a stored hash with that hash's own
// algorithm, and an unknown email burns a dummy made by DefaultHasher. A
// bcrypt placeholder under DefaultHasher = Argon2Hasher answered in bcrypt
// time while an unknown email answered in argon2 time, and the gap told an
// attacker which addresses hold a passwordless account (CWE-208).
func placeholderPasswordHash() string {
	h := DefaultHasher
	if h == nil {
		return passwordPlaceholderHash
	}
	key := hasherKey(h)
	if v, ok := placeholderHashes.Load(key); ok {
		return v.(string)
	}
	hash, err := newPlaceholderHashWith(h)
	if err != nil {
		return passwordPlaceholderHash
	}
	v, _ := placeholderHashes.LoadOrStore(key, hash)
	return v.(string)
}

// newPlaceholderHash is an unusable hash of a fresh random secret, made by
// DefaultHasher for the reason placeholderPasswordHash gives. Clearing a
// password writes one instead of the shared placeholder: an account
// created without a password already holds that, so writing it back would
// leave the stored hash, and the credential tag of every JWT issued before
// the clear, unchanged.
func newPlaceholderHash() (string, error) {
	h := DefaultHasher
	if h == nil {
		h = BcryptHasher{}
	}
	return newPlaceholderHashWith(h)
}

// newPlaceholderHashWith hashes 32 random bytes, hex-encoded (64 bytes,
// under bcrypt's 72-byte limit), with h.
func newPlaceholderHashWith(h PasswordHasher) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return h.Hash(hex.EncodeToString(secret))
}

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing dummy bcrypt hash: %v", err))
	}
	dummyBcryptHash = string(h)

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("auth: generating placeholder secret: %v", err))
	}
	hp, err := bcrypt.GenerateFromPassword(secret, bcryptCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing placeholder hash: %v", err))
	}
	passwordPlaceholderHash = string(hp)
	placeholderHashes.Store(hasherKey(BcryptHasher{}), passwordPlaceholderHash)
}
