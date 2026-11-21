package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

func TestDefaultKeyValidatorRefusesBackslash(t *testing.T) {
	if err := (DefaultKeyValidator{}).ValidateKey(`a\b.png`); !errors.Is(err, upload.ErrInvalidKey) {
		t.Fatalf("ValidateKey(a\\b.png) = %v, want upload.ErrInvalidKey", err)
	}
}

func TestMemorySaveRefusesOversizeBody(t *testing.T) {
	const limit = 50 << 20
	m := NewMemoryStorage()
	err := m.Save(context.Background(), "big", io.LimitReader(zeroReader{}, limit+1))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Save of 50 MiB + 1 bytes = %v, want a too-large error", err)
	}
	if err := m.Save(context.Background(), "full", io.LimitReader(zeroReader{}, limit)); err != nil {
		t.Fatalf("Save of exactly 50 MiB = %v, want it stored", err)
	}
	if ok, _ := m.Exists(context.Background(), "big"); ok {
		t.Fatal("an oversize body was stored")
	}
}

func TestMemorySaveCanceledMidCopyKeepsObject(t *testing.T) {
	m := NewMemoryStorage()
	_ = m.Save(context.Background(), "a", strings.NewReader("old"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if m.Save(ctx, "a", cancelingReader{strings.NewReader("new"), cancel}) == nil {
		t.Fatal("Save canceled mid-copy succeeded")
	}
	rc, _ := m.Get(context.Background(), "a")
	if b, _ := io.ReadAll(rc); !bytes.Equal(b, []byte("old")) {
		t.Fatalf("object = %q, want old", b)
	}
}

func TestCloseClosesThePinnedRoot(t *testing.T) {
	ls := NewLocalStorage(t.TempDir())
	if err := ls.Save(context.Background(), "a", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	pinned := ls.root
	if pinned == nil {
		t.Fatal("Save pinned no root")
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pinned.Stat("."); err == nil {
		t.Fatal("the pinned root is still open after Close")
	}
}

func TestVerifyChecksumRefusesShortDigest(t *testing.T) {
	m := NewMemoryStorage()
	_ = m.Save(context.Background(), "a", strings.NewReader("x"))
	err := VerifyChecksum(context.Background(), m, "a", strings.Repeat("a", 62))
	if err == nil || errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("VerifyChecksum with a 62-character digest = %v, want a malformed-digest error", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
