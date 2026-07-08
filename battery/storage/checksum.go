package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrChecksumMismatch is the sentinel returned (wrapped) by
// [VerifyChecksum] when an object's actual SHA-256 digest does not
// match the expected digest.
var ErrChecksumMismatch = errors.New("storage: checksum mismatch")

// SaveResult reports what [SaveWithChecksum] wrote.
type SaveResult struct {
	// Size is the number of bytes written.
	Size int64
	// SHA256 is the lowercase hex SHA-256 digest of the stored content.
	SHA256 string
}

// SaveWithChecksum stores r under key via s.Save while teeing the stream
// through a SHA-256 hasher, so the content is read exactly once and no
// backend changes are required. It returns a [SaveResult] carrying the
// byte count and the lowercase hex digest. On a Save error it returns
// the zero SaveResult and the error from s.Save; the stream is not
// buffered in memory, so it works for arbitrarily large objects.
func SaveWithChecksum(ctx context.Context, s Storage, key string, r io.Reader) (SaveResult, error) {
	h := sha256.New()
	cr := &countReader{r: io.TeeReader(r, h)}
	if err := s.Save(ctx, key, cr); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Size: cr.n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// VerifyChecksum re-reads key from s and compares the content's SHA-256
// digest with wantSHA256. wantSHA256 must be exactly 64 hexadecimal
// characters; both lower- and uppercase hex are accepted. It returns nil
// on a match, an error wrapping [ErrChecksumMismatch] (carrying the key
// and the got/want digests) on a mismatch, and the underlying error if
// the object cannot be read or wantSHA256 is malformed.
func VerifyChecksum(ctx context.Context, s Storage, key, wantSHA256 string) error {
	want, err := normalizeSha256Hex(wantSHA256)
	if err != nil {
		return fmt.Errorf("storage: invalid expected checksum for %q: %w", key, err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("storage: read %q for checksum: %w", key, err)
	}
	defer rc.Close()

	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return fmt.Errorf("storage: hashing %q: %w", key, err)
	}
	got := hex.EncodeToString(h.Sum(nil))

	if got != want {
		return fmt.Errorf("storage: checksum mismatch for %q: got %s, want %s: %w",
			key, got, want, ErrChecksumMismatch)
	}
	return nil
}

// countReader counts the bytes read through r. It is used to obtain the
// byte count alongside the io.TeeReader hash in a single pass.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// normalizeSha256Hex validates that s is a 64-character hexadecimal
// SHA-256 digest and returns its lowercase form, so callers can compare
// digests with a plain equality check.
func normalizeSha256Hex(s string) (string, error) {
	const hexLen = sha256.Size * 2 // 64
	if len(s) != hexLen {
		return "", fmt.Errorf("expected %d hex characters, got %d", hexLen, len(s))
	}
	lo := strings.ToLower(s)
	if _, err := hex.DecodeString(lo); err != nil {
		return "", fmt.Errorf("not valid hexadecimal: %w", err)
	}
	return lo, nil
}
