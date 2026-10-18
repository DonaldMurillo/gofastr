package semantic

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// snapshotVersion is bumped when the on-disk format changes in a way
// that requires a code path to load old files.
const snapshotVersion = 1

// snapshotHeader is the first gob value in a snapshot file. The model
// fingerprint is checked at load time, refusing to mix vectors from
// different models is more important than convenience here.
type snapshotHeader struct {
	Version    int
	Model      string
	Dim        int
	ChunkCount int
	CreatedAt  int64 // unix seconds
}

// Snapshot writes a complete, atomic snapshot of the store to path.
// The write goes to path+".tmp" and is renamed into place; partial
// snapshots are never observable.
func (s *FlatStore) Snapshot(path string) error {
	if path == "" {
		return errors.New("semantic: Snapshot path is required")
	}
	// Owner-only dir: the snapshot carries the full document corpus
	// (and its vectors), which is app data, not a shared artifact.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("semantic: mkdir snapshot dir: %w", err)
	}
	if err := fileperm.Restrict(filepath.Dir(path), true); err != nil {
		return fmt.Errorf("semantic: restrict snapshot dir: %w", err)
	}

	s.mu.RLock()
	header := snapshotHeader{
		Version:    snapshotVersion,
		Model:      s.model,
		Dim:        s.dim,
		ChunkCount: len(s.chunks),
		CreatedAt:  time.Now().Unix(),
	}
	chunks := make([]Chunk, len(s.chunks))
	copy(chunks, s.chunks)
	s.mu.RUnlock()

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("semantic: create snapshot tmp: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := fileperm.RestrictOpenFile(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("semantic: restrict snapshot tmp: %w", err)
	}
	enc := gob.NewEncoder(f)
	if err := enc.Encode(header); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("semantic: encode header: %w", err)
	}
	for i := range chunks {
		if err := enc.Encode(chunks[i]); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("semantic: encode chunk %d: %w", i, err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("semantic: fsync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("semantic: close snapshot: %w", err)
	}
	if err := atomicfile.Replace(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("semantic: rename snapshot into place: %w", err)
	}
	return nil
}

// LoadSnapshot replaces the store's contents with the snapshot at
// path. If the snapshot was produced by a different embedder model or
// dimension, [ErrModelMismatch] is returned and the store is left
// untouched.
func (s *FlatStore) LoadSnapshot(path string) error {
	f, err := atomicfile.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("semantic: open snapshot: %w", err)
	}
	defer f.Close()

	dec := gob.NewDecoder(f)
	var header snapshotHeader
	if err := dec.Decode(&header); err != nil {
		return fmt.Errorf("semantic: decode header: %w", err)
	}
	if header.Version != snapshotVersion {
		return fmt.Errorf("semantic: snapshot version %d, this build supports %d", header.Version, snapshotVersion)
	}
	// The store can be constructed with dim=0 when the embedder defers
	// dimension discovery to first use (OllamaEmbedder probes lazily).
	// In that case adopt the snapshot's dim, it's the source of truth.
	// A non-zero store dim that disagrees with the snapshot is still a
	// loud refusal.
	if header.ChunkCount < 0 || header.Dim < 0 {
		return errors.New("semantic: snapshot count and dimension must be nonnegative")
	}
	s.mu.RLock()
	storeDim, storeModel := s.dim, s.model
	s.mu.RUnlock()
	if header.Model != storeModel || (storeDim != 0 && header.Dim != storeDim) {
		return &ModelMismatchError{SnapshotModel: header.Model, SnapshotDim: header.Dim, StoreModel: storeModel, StoreDim: storeDim}
	}
	chunks := make([]Chunk, 0)
	byDoc := make(map[string][]int)
	for {
		var c Chunk
		err := dec.Decode(&c)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("semantic: decode chunk: %w", err)
		}
		if len(chunks) >= header.ChunkCount {
			return errors.New("semantic: snapshot contains more chunks than its header")
		}
		if len(c.Vec) != header.Dim {
			return errVecDim(header.Dim, len(c.Vec))
		}
		if err := finiteVector(c.Vec); err != nil {
			return err
		}
		normalize(c.Vec)
		idx := len(chunks)
		chunks = append(chunks, c)
		byDoc[c.DocID] = append(byDoc[c.DocID], idx)
	}

	if len(chunks) != header.ChunkCount {
		return errors.New("semantic: snapshot chunk count does not match its header")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dim != 0 && s.dim != header.Dim {
		return errVecDim(s.dim, header.Dim)
	}
	s.dim = header.Dim
	s.chunks = chunks
	s.byDoc = byDoc
	return nil
}

// ModelMismatchError is returned when a persisted snapshot was produced
// by a different embedder than the one configured on the store. Mixing
// vectors from different models is silently catastrophic for retrieval
// quality, so the load is refused loudly instead.
type ModelMismatchError struct {
	SnapshotModel, StoreModel string
	SnapshotDim, StoreDim     int
}

func (e *ModelMismatchError) Error() string {
	return fmt.Sprintf(
		"semantic: snapshot embedded with model=%q dim=%d, store configured with model=%q dim=%d: re-index after model changes",
		e.SnapshotModel, e.SnapshotDim, e.StoreModel, e.StoreDim,
	)
}

// walOp is the discriminator for [walEntry].
type walOp string

const (
	walOpAdd    walOp = "add"
	walOpRemove walOp = "remove"
)

// walEntry is one record in the write-ahead log. Add carries a copy of
// the chunks (including their vectors) so replay does not need to call
// the embedder. Remove carries the doc ID; chunk-level deletion is
// always doc-scoped.
type walEntry struct {
	Op     walOp
	DocID  string
	Chunks []Chunk
}

// wal is a tiny append-only write-ahead log. It is not high-performance.
// One fsync per write, but it is correct and easy to reason about,
// and at 100k chunks the snapshot path dominates throughput anyway.
type wal struct {
	mu   sync.Mutex
	path string
	f    *os.File
	enc  *gob.Encoder
}

// Supply ReadByte so gob does not read ahead past a completed record;
// the file offset then identifies the durable prefix of a torn log.
type walReplayReader struct{ *os.File }

func (r walReplayReader) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r.File, b[:])
	return b[0], err
}

func openWAL(path string) (*wal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("semantic: mkdir wal dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("semantic: open wal: %w", err)
	}
	if err := fileperm.Restrict(path, false); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("semantic: restrict wal: %w", err)
	}
	return &wal{path: path, f: f, enc: gob.NewEncoder(f)}, nil
}

func (w *wal) append(e walEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("semantic: WAL is closed")
	}
	if err := w.enc.Encode(e); err != nil {
		return fmt.Errorf("semantic: wal append: %w", err)
	}
	return w.f.Sync()
}

func (w *wal) replay(apply func(walEntry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("semantic: WAL is closed")
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("semantic: wal seek: %w", err)
	}
	dec := gob.NewDecoder(walReplayReader{w.f})
	var lastGood int64
	for {
		var e walEntry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// Use a separate write handle: Windows append handles cannot
			// call SetEndOfFile, but os.Truncate opens the required handle.
			if err := os.Truncate(w.path, lastGood); err != nil {
				return fmt.Errorf("semantic: truncate torn WAL record: %w", err)
			}
			if err := w.f.Sync(); err != nil {
				return err
			}
			break
		}
		if err != nil {
			return fmt.Errorf("semantic: wal decode: %w", err)
		}
		if err := apply(e); err != nil {
			return fmt.Errorf("semantic: wal apply: %w", err)
		}
		lastGood, err = w.f.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
	}
	// Seek to end so further appends go after the replayed entries.
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("semantic: wal seek end: %w", err)
	}
	w.enc = gob.NewEncoder(w.f)
	return nil
}

// truncate empties the WAL. Call after a successful snapshot so the
// next replay does not double-apply already-snapshotted entries.
func (w *wal) truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("semantic: WAL is closed")
	}
	// Windows refuses SetEndOfFile on a handle opened with append semantics.
	// Close and reopen with O_TRUNC while holding the WAL lock; this is also a
	// cleaner reset boundary for the gob encoder on every platform.
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("semantic: wal sync before truncate: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("semantic: wal close before truncate: %w", err)
	}
	w.f = nil
	f, err := os.OpenFile(w.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		// Best-effort recovery keeps the WAL object usable even though the
		// snapshot caller still receives the reset failure.
		w.f, _ = os.OpenFile(w.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
		if w.f != nil {
			w.enc = gob.NewEncoder(w.f)
		}
		return fmt.Errorf("semantic: wal reopen after truncate: %w", err)
	}
	w.f = f
	w.enc = gob.NewEncoder(f)
	return f.Sync()
}

func (w *wal) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
