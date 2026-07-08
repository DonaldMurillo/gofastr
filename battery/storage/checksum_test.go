package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSaveWithChecksumKnownVector(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	content := []byte("hello world")

	res, err := SaveWithChecksum(ctx, ms, "k.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("SaveWithChecksum: %v", err)
	}
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if res.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", res.SHA256, want)
	}
	if res.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", res.Size, len(content))
	}
	// Object must be retrievable with the same bytes.
	rc, err := ms.Get(ctx, "k.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("retrieved content = %q, want %q", got, content)
	}
}

func TestSaveWithChecksumEmpty(t *testing.T) {
	ms := NewMemoryStorage()
	res, err := SaveWithChecksum(context.Background(), ms, "empty", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("SaveWithChecksum: %v", err)
	}
	if res.Size != 0 {
		t.Errorf("Size = %d, want 0", res.Size)
	}
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if res.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", res.SHA256, want)
	}
}

func TestSaveWithChecksumLargeStream(t *testing.T) {
	ms := NewMemoryStorage()
	const size int64 = 5 << 20 // 5 MiB

	// Two equivalent generators: one to compute the reference digest
	// directly, one to hand to SaveWithChecksum. Guards against the
	// helper buffering (or short-reading) the stream.
	ref := sha256.New()
	n, _ := io.Copy(ref, &patternStream{remain: size})
	want := hex.EncodeToString(ref.Sum(nil))

	res, err := SaveWithChecksum(context.Background(), ms, "big.bin", &patternStream{remain: size})
	if err != nil {
		t.Fatalf("SaveWithChecksum: %v", err)
	}
	if res.Size != n {
		t.Errorf("Size = %d, want %d", res.Size, n)
	}
	if res.SHA256 != want {
		t.Errorf("SHA256 = %q, want %q", res.SHA256, want)
	}
}

func TestVerifyChecksumMatch(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	res, _ := SaveWithChecksum(ctx, ms, "k.txt", bytes.NewReader([]byte("hello world")))
	if err := VerifyChecksum(ctx, ms, "k.txt", res.SHA256); err != nil {
		t.Fatalf("VerifyChecksum on match returned %v, want nil", err)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	SaveWithChecksum(ctx, ms, "k.txt", bytes.NewReader([]byte("hello world")))

	// The digest of different content must not validate "k.txt".
	other := sha256Hex([]byte("totally different content"))
	err := VerifyChecksum(ctx, ms, "k.txt", other)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestVerifyChecksumMissingKey(t *testing.T) {
	ms := NewMemoryStorage()
	err := VerifyChecksum(context.Background(), ms, "does-not-exist",
		"b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("missing key must NOT be ErrChecksumMismatch, got %v", err)
	}
}

func TestVerifyChecksumMalformedHex(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	SaveWithChecksum(ctx, ms, "k.txt", bytes.NewReader([]byte("x")))

	cases := []string{
		"abc",                         // too short
		strings.Repeat("0", 63),       // wrong length, valid hex prefix
		strings.Repeat("z", 64),       // right length, non-hex
		"g" + strings.Repeat("0", 63), // right length, non-hex
	}
	for _, c := range cases {
		err := VerifyChecksum(ctx, ms, "k.txt", c)
		if err == nil {
			t.Errorf("VerifyChecksum(%q) = nil, want validation error", c)
		}
		if errors.Is(err, ErrChecksumMismatch) {
			t.Errorf("VerifyChecksum(%q) is ErrChecksumMismatch; want a parse error", c)
		}
	}
}

func TestVerifyChecksumUppercaseHex(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	digest := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	SaveWithChecksum(ctx, ms, "k.txt", bytes.NewReader([]byte("hello world")))
	if err := VerifyChecksum(ctx, ms, "k.txt", strings.ToUpper(digest)); err != nil {
		t.Fatalf("uppercase hex should be accepted, got %v", err)
	}
}

func TestSaveWithChecksumSaveError(t *testing.T) {
	var s errStorage
	res, err := SaveWithChecksum(context.Background(), s, "k", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected Save error, got nil")
	}
	if res.Size != 0 || res.SHA256 != "" {
		t.Errorf("expected zero SaveResult on Save error, got %+v", res)
	}
}

// sha256Hex returns the lowercase hex SHA-256 digest of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// errStorage is a Storage whose Save always fails, for error propagation.
type errStorage struct{}

func (errStorage) Save(context.Context, string, io.Reader) error { return errors.New("disk full") }
func (errStorage) Delete(context.Context, string) error          { return nil }
func (errStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not found")
}
func (errStorage) Exists(context.Context, string) (bool, error) { return false, nil }

// patternStream emits remain bytes of a deterministic, repeating 0..255
// pattern. Two instances with the same remain value produce identical
// streams, so a reference digest can be computed independently.
type patternStream struct {
	remain int64
	b      byte
}

func (s *patternStream) Read(p []byte) (int, error) {
	if s.remain <= 0 {
		return 0, io.EOF
	}
	max := int64(len(p))
	if s.remain < max {
		max = s.remain
	}
	for i := int64(0); i < max; i++ {
		p[i] = s.b
		s.b++
	}
	s.remain -= max
	return int(max), nil
}
