package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// DefaultKeyValidator's doc said it prevents traversal, yet it passed an
// absolute key (/etc/passwd) and a NUL byte, and S3Storage refused only
// "..": a//b, ./a and a leading / reached the bucket as keys distinct
// from the object they spell, so a row naming one erased or served an
// object the row's key never named. Property: both refuse an absolute key,
// a control byte, and a key that is not its own clean form, with
// upload.ErrInvalidKey; clean relative keys pass.
var unformedKeys = []string{
	"/etc/passwd",
	"/a/b.png",
	"a\x00b.png",
	"a\nb.png",
	"a\x7fb.png",
	"a//b.png",
	"./a.png",
	"a/./b.png",
	"a/b/",
	".",
}

var formedKeys = []string{"a.png", "uploads/posts/photo/x_1_abc.png", "a b/c-d_e.txt"}

func TestDefaultKeyValidatorRefusesUnformedKeys(t *testing.T) {
	var v DefaultKeyValidator
	for _, k := range unformedKeys {
		if err := v.ValidateKey(k); !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("ValidateKey(%q) = %v, want upload.ErrInvalidKey", k, err)
		}
	}
	for _, k := range formedKeys {
		if err := v.ValidateKey(k); err != nil {
			t.Errorf("ValidateKey(%q) refused a clean key: %v", k, err)
		}
	}
}

func TestS3RefusesUnformedKeys(t *testing.T) {
	ctx := context.Background()
	s := NewS3Storage("bucket", "us-east-1", WithS3Client(newMockS3Client()), WithPresigner(&mockPresigner{}))
	ops := map[string]func(string) error{
		"Save":   func(k string) error { return s.Save(ctx, k, bytes.NewReader([]byte("x"))) },
		"Get":    func(k string) error { _, err := s.Get(ctx, k); return err },
		"Delete": func(k string) error { return s.Delete(ctx, k) },
		"Exists": func(k string) error { _, err := s.Exists(ctx, k); return err },
		"PresignedGetURL": func(k string) error {
			_, err := s.PresignedGetURL(ctx, k, time.Hour)
			return err
		},
	}
	for name, op := range ops {
		for _, k := range unformedKeys {
			if err := op(k); !errors.Is(err, upload.ErrInvalidKey) {
				t.Errorf("%s(%q) = %v, want upload.ErrInvalidKey", name, k, err)
			}
		}
	}
	for _, k := range formedKeys {
		if err := ops["Save"](k); err != nil {
			t.Errorf("Save(%q) refused a clean key: %v", k, err)
		}
	}
}

// A zero or negative expiry is no expiry a presigner can honor: an SDK
// that clamps it to its own default signs a URL for far longer than the
// caller asked, and one that passes it through signs a URL that is dead
// on arrival. Property: both presign calls refuse expires <= 0.
func TestS3PresignRefusesNonPositiveExpiry(t *testing.T) {
	ctx := context.Background()
	s := NewS3Storage("bucket", "us-east-1", WithPresigner(&mockPresigner{}))
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := s.PresignedGetURL(ctx, "a.txt", d); err == nil {
			t.Errorf("PresignedGetURL(expires=%v) succeeded; want a refusal", d)
		}
		if _, err := s.PresignedPutURL(ctx, "a.txt", d); err == nil {
			t.Errorf("PresignedPutURL(expires=%v) succeeded; want a refusal", d)
		}
	}
	if _, err := s.PresignedGetURL(ctx, "a.txt", time.Minute); err != nil {
		t.Fatalf("a positive expiry was refused: %v", err)
	}
}
