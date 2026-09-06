package file_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// Pins the rollback-ledger contract of recordingStore, found by the
// 2026-09-05 red-probe round (round 4); fixed by unrecording a key in
// recordingStore.Delete only when the underlying delete removed the
// object (nil or os.ErrNotExist), so a write the deriver could not undo
// itself stays on the ledger and ProcessFileField's cleanupOrphans
// retries it. Property: a failed Delete must not unrecord a key that
// still exists in storage — the rejected upload must leave nothing
// behind.

// flakyOnceStore is an upload.Storage whose FIRST Delete fails (a
// transient backend error mid-undo) and every later call behaves
// normally, so the only object that can survive is one nobody retried.
type flakyOnceStore struct {
	saved     map[string][]byte
	deleteHit bool
}

func newFlakyOnceStore() *flakyOnceStore {
	return &flakyOnceStore{saved: map[string][]byte{}}
}

func (s *flakyOnceStore) Save(_ context.Context, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.saved[key] = data
	return nil
}

func (s *flakyOnceStore) Delete(_ context.Context, key string) error {
	if !s.deleteHit {
		s.deleteHit = true
		return errors.New("flaky: transient delete failure")
	}
	delete(s.saved, key)
	return nil
}

func (s *flakyOnceStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.saved[key]
	if !ok {
		return nil, errors.New("flaky: missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *flakyOnceStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.saved[key]
	return ok, nil
}

var _ upload.Storage = (*flakyOnceStore)(nil)

// undoFailingDeriver saves one rendition, tries (and fails) to undo it
// through the same store view, then fails the upload — a third-party
// deriver whose own cleanup hit a backend error halfway.
type undoFailingDeriver struct{}

func (undoFailingDeriver) DeriveImage(ctx context.Context, store upload.Storage, _ []byte, primaryRef string) (*file.ImageDerivatives, error) {
	key := primaryRef + ".sm"
	if err := store.Save(ctx, key, bytes.NewReader([]byte("rendition"))); err != nil {
		return nil, err
	}
	if err := store.Delete(ctx, key); err != nil {
		// The undo failed; the deriver gives up and fails the upload.
		return nil, err
	}
	return &file.ImageDerivatives{}, nil
}

// TestDeriverFailedUndoStillRolledBack pins that a rendition whose
// deriver-issued Delete failed stays on the rollback ledger: the
// rejected upload must leave nothing in storage.
func TestDeriverFailedUndoStillRolledBack(t *testing.T) {
	store := newFlakyOnceStore()
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}

	_, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(png),
		"photo.png", "posts", "cover", file.WithImageDeriver(undoFailingDeriver{}))
	if err == nil {
		t.Fatal("ProcessFileField unexpectedly succeeded; test setup is wrong")
	}

	for key, data := range store.saved {
		t.Errorf("SECURITY: [filefield-undo-ledger] rejected upload left object %q (%d bytes) in storage while the primary was rolled back. Attack: the deriver saved a rendition, its own Delete failed transiently (object still stored), and recordingStore.Delete unrecorded the key anyway, so ProcessFileField's cleanupOrphans never retried it — the rejected-upload orphan class TestProcessFileFieldDeriveFailureOrphans pins, reopened through the undo ledger.", key, len(data))
	}
}
