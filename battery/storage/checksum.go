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

// ErrChecksumMismatch is wrapped by [VerifyChecksum] when the digests differ.
var ErrChecksumMismatch = errors.New("storage: checksum mismatch")

// SaveResult reports what [SaveWithChecksum] wrote.
type SaveResult struct {
	// Size is the number of bytes written.
	Size int64
	// SHA256 is the lowercase hex SHA-256 digest of the stored content.
	SHA256 string
}

// SaveWithChecksum stores r under key and returns its size and SHA-256 digest.
func SaveWithChecksum(ctx context.Context, s Storage, key string, r io.Reader) (SaveResult, error) {
	h := sha256.New()
	cr := &countReader{r: io.TeeReader(r, h)}
	if err := s.Save(ctx, key, cr); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Size: cr.n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// VerifyChecksum re-reads key and compares its SHA-256 digest with wantSHA256.
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
	if _, err := io.Copy(h, checksumReader{ctx, rc}); err != nil {
		return fmt.Errorf("storage: hashing %q: %w", key, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))

	if got != want {
		return fmt.Errorf("storage: checksum mismatch for %q: got %s, want %s: %w",
			key, got, want, ErrChecksumMismatch)
	}
	return nil
}

type checksumReader struct {
	ctx context.Context
	r   io.Reader
}

func (r checksumReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
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

func normalizeSha256Hex(s string) (string, error) {
	const hexLen = sha256.Size * 2
	if len(s) != hexLen {
		return "", fmt.Errorf("expected %d hex characters, got %d", hexLen, len(s))
	}
	lo := strings.ToLower(s)
	if _, err := hex.DecodeString(lo); err != nil {
		return "", fmt.Errorf("not valid hexadecimal: %w", err)
	}
	return lo, nil
}
