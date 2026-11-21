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

// RecommendedMinPasswordBytes is the minimum password length, in characters, ValidatePasswordStrength enforces.
const RecommendedMinPasswordBytes = 8

// ErrPasswordEmpty is HashPassword's and ValidatePasswordStrength's error for an empty password.
var ErrPasswordEmpty error = i18n.NewError(reasonPasswordEmpty)

// ErrPasswordTooShort is ValidatePasswordStrength's error for a password under the minimum length.
var ErrPasswordTooShort error = i18n.NewError(reasonPasswordTooShortField)

// PasswordHasher hashes and verifies passwords; BcryptHasher and Argon2Hasher ship with the package.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) bool
}

// DefaultHasher hashes new passwords; set it before Init.
var DefaultHasher PasswordHasher = BcryptHasher{}

// HashPassword hashes password with DefaultHasher, or returns ErrPasswordEmpty.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	return DefaultHasher.Hash(password)
}

// ValidatePasswordStrength returns ErrPasswordEmpty or ErrPasswordTooShort, counting characters.
func ValidatePasswordStrength(password string) error {
	if password == "" {
		return ErrPasswordEmpty
	}
	if utf8.RuneCountInString(password) < RecommendedMinPasswordBytes {
		return ErrPasswordTooShort
	}
	return nil
}

// CheckPassword reports whether password matches hash, a bcrypt or argon2id hash.
func CheckPassword(password, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return Argon2Hasher{}.Verify(password, hash)
	}
	return BcryptHasher{}.Verify(password, hash)
}

func verifyPassword(password, hash string) bool {
	if isCustomHasher(DefaultHasher) && DefaultHasher.Verify(password, hash) {
		return true
	}
	return CheckPassword(password, hash)
}

func isCustomHasher(h PasswordHasher) bool {
	switch h.(type) {
	case nil, BcryptHasher, *BcryptHasher, Argon2Hasher, *Argon2Hasher:
		return false
	}
	return true
}

// BcryptHasher hashes with bcrypt, pre-hashing inputs over 72 bytes with SHA-256; it is the default.
type BcryptHasher struct{}

// Hash returns a bcrypt hash, or ErrPasswordEmpty.
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

// Verify reports whether password matches the bcrypt hash.
func (BcryptHasher) Verify(password, hash string) bool {
	input := []byte(password)
	if len(input) > 72 {
		sum := sha256.Sum256(input)
		input = []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), input) == nil
}

// Argon2Hasher hashes with argon2id into a PHC string; zero fields take OWASP defaults (64 MiB, t=3, p=2, 32-byte key).
type Argon2Hasher struct {
	Time    uint32 // iterations.     0 → 3.
	Memory  uint32 // KiB.            0 → 65536 (64 MiB).
	Threads uint8  // parallelism.    0 → 2.
	KeyLen  uint32 // output bytes.   0 → 32.
}

// Hash returns an argon2id PHC hash with a random salt, refusing parameters Verify would refuse.
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

// Verify reports whether password matches the PHC hash; a malformed or oversized hash is false.
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

type argon2PHC struct {
	time, memory uint32
	threads      uint8
	salt, key    []byte
}

const (
	maxArgon2MemoryKiB uint32 = 1 << 20 // 1 GiB
	maxArgon2Time      uint32 = 100
	maxArgon2Threads   uint8  = 16
	maxArgon2KeyLen    uint32 = 128
)

func parseArgon2PHC(hash string) (argon2PHC, bool) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2PHC{}, false
	}
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
	if p.memory > maxArgon2MemoryKiB || p.time > maxArgon2Time || p.threads > maxArgon2Threads || uint32(len(key)) > maxArgon2KeyLen {
		return argon2PHC{}, false
	}
	p.salt, p.key = salt, key
	return p, true
}

var bcryptCost = bcrypt.DefaultCost

var dummyBcryptHash string

var dummyHashes sync.Map

func hasherKey(h PasswordHasher) string {
	switch h.(type) {
	case BcryptHasher, *BcryptHasher, Argon2Hasher, *Argon2Hasher:
		return fmt.Sprintf("%T|%+v", h, h)
	}
	return fmt.Sprintf("%T", h)
}

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

var passwordPlaceholderHash string

var placeholderHashes sync.Map

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

func newPlaceholderHash() (string, error) {
	h := DefaultHasher
	if h == nil {
		h = BcryptHasher{}
	}
	return newPlaceholderHashWith(h)
}

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
