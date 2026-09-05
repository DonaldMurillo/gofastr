package upload_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins torn reads of an in-flight Save, found by the 2026-09-04
// red-probe round; fixed by writing Save's data to a temp file beside
// the target and renaming it into place, so the final path flips
// atomically and never exposes a half-written object.
// Property: an object must not become visible to readers until Save has completed — a
// key's content is either absent or whole, never the torn window of an in-flight write.
// battery/storage's LocalStorage holds this property by construction ("Writes are atomic:
// data is first written to a temporary file, then renamed to the final path")
// and pins it as TestLocalStorageAtomicWrite; core/upload's LocalStorage, the
// backend of the documented ServeHandler wiring, holds the same line.
// Surfaces: core/upload/local.go:Save (temp file + rename) as observed through
// core/upload/local.go:open (Get/GetRange) on the same key.

// stallReader delivers one chunk, signals, and blocks until released — the deterministic
// rendezvous pattern (no sleeps) from battery/auth/twofa_security_test.go.
type stallReader struct {
	wroteFirst chan struct{}
	release    chan struct{}
	done1      bool
}

func (r *stallReader) Read(p []byte) (int, error) {
	if !r.done1 {
		r.done1 = true
		n := copy(p, "PARTIAL-")
		close(r.wroteFirst)
		return n, nil
	}
	<-r.release
	return 0, io.EOF
}

func TestSaveNotVisibleUntilComplete(t *testing.T) {
	base := t.TempDir()
	store := upload.NewLocalStorage(base)
	ctx := context.Background()

	r := &stallReader{wroteFirst: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- store.Save(ctx, "avatars/u1.bin", r)
	}()

	// Wait until the first chunk has been flushed into the temp file,
	// then read the same key while the writer is still stalled inside
	// io.Copy: the final path must not exist yet.
	select {
	case <-r.wroteFirst:
	case <-time.After(5 * time.Second):
		close(r.release)
		<-done
		t.Fatal("Save never delivered its first chunk; test setup is wrong")
	}

	rc, err := store.Get(ctx, "avatars/u1.bin")
	if err == nil {
		body, _ := io.ReadAll(rc)
		rc.Close()
		close(r.release)
		<-done
		if len(body) != 0 {
			t.Errorf("SECURITY: [upload-torn-read] Get on key %q returned %d byte(s) (\"%s\") while Save was still in flight: a concurrent reader (the documented ServeHandler pairing) must never be served a torn object. battery/storage's temp-file+rename backend holds this invariant; this backend does too.", "avatars/u1.bin", len(body), string(body))
		}
		return
	}
	// Save not yet complete: the key must read as absent (or the PREVIOUS
	// whole object), never partial. Absent is the correct outcome while
	// the write is in flight; both accepted fix shapes pass this leg.
	close(r.release)
	if err := <-done; err != nil {
		t.Fatalf("background Save failed: %v", err)
	}

	// After completion the object must be whole and readable.
	rc, err = store.Get(ctx, "avatars/u1.bin")
	if err != nil {
		t.Fatalf("Get after completed Save: %v", err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "PARTIAL-" {
		t.Fatalf("post-completion body = %q, want PARTIAL-; test setup is wrong", string(body))
	}
}
