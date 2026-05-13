package embed

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// snapshotVersion is bumped when the on-disk format changes in a way
// that requires a code path to load old files.
const snapshotVersion = 1

// snapshotHeader is the first gob value in a snapshot file. The model
// fingerprint is checked at load time — refusing to mix vectors from
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
		return errors.New("embed: Snapshot path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("embed: mkdir snapshot dir: %w", err)
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

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("embed: create snapshot tmp: %w", err)
	}
	enc := gob.NewEncoder(f)
	if err := enc.Encode(header); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("embed: encode header: %w", err)
	}
	for i := range chunks {
		if err := enc.Encode(chunks[i]); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("embed: encode chunk %d: %w", i, err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("embed: fsync snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("embed: close snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("embed: rename snapshot into place: %w", err)
	}
	return nil
}

// LoadSnapshot replaces the store's contents with the snapshot at
// path. If the snapshot was produced by a different embedder model or
// dimension, [ErrModelMismatch] is returned and the store is left
// untouched.
func (s *FlatStore) LoadSnapshot(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("embed: open snapshot: %w", err)
	}
	defer f.Close()

	dec := gob.NewDecoder(f)
	var header snapshotHeader
	if err := dec.Decode(&header); err != nil {
		return fmt.Errorf("embed: decode header: %w", err)
	}
	if header.Version != snapshotVersion {
		return fmt.Errorf("embed: snapshot version %d, this build supports %d", header.Version, snapshotVersion)
	}
	// The store can be constructed with dim=0 when the embedder defers
	// dimension discovery to first use (OllamaEmbedder probes lazily).
	// In that case adopt the snapshot's dim — it's the source of truth.
	// A non-zero store dim that disagrees with the snapshot is still a
	// loud refusal.
	if header.Model != s.model || (s.dim != 0 && header.Dim != s.dim) {
		return &ModelMismatchError{
			SnapshotModel: header.Model, SnapshotDim: header.Dim,
			StoreModel: s.model, StoreDim: s.dim,
		}
	}
	if s.dim == 0 {
		s.dim = header.Dim
	}

	chunks := make([]Chunk, 0, header.ChunkCount)
	byDoc := make(map[string][]int)
	for {
		var c Chunk
		err := dec.Decode(&c)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("embed: decode chunk: %w", err)
		}
		idx := len(chunks)
		chunks = append(chunks, c)
		byDoc[c.DocID] = append(byDoc[c.DocID], idx)
	}

	s.mu.Lock()
	s.chunks = chunks
	s.byDoc = byDoc
	s.mu.Unlock()
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
		"embed: snapshot embedded with model=%q dim=%d, store configured with model=%q dim=%d — re-index after model changes",
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

// wal is a tiny append-only write-ahead log. It is not high-performance
// — one fsync per write — but it is correct and easy to reason about,
// and at 100k chunks the snapshot path dominates throughput anyway.
type wal struct {
	mu   sync.Mutex
	path string
	f    *os.File
	enc  *gob.Encoder
}

func openWAL(path string) (*wal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("embed: mkdir wal dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("embed: open wal: %w", err)
	}
	return &wal{path: path, f: f, enc: gob.NewEncoder(f)}, nil
}

func (w *wal) append(e walEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(e); err != nil {
		return fmt.Errorf("embed: wal append: %w", err)
	}
	return w.f.Sync()
}

func (w *wal) replay(apply func(walEntry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("embed: wal seek: %w", err)
	}
	dec := gob.NewDecoder(w.f)
	for {
		var e walEntry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("embed: wal decode: %w", err)
		}
		if err := apply(e); err != nil {
			return fmt.Errorf("embed: wal apply: %w", err)
		}
	}
	// Seek to end so further appends go after the replayed entries.
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("embed: wal seek end: %w", err)
	}
	w.enc = gob.NewEncoder(w.f)
	return nil
}

// truncate empties the WAL. Call after a successful snapshot so the
// next replay does not double-apply already-snapshotted entries.
func (w *wal) truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.f.Truncate(0); err != nil {
		return fmt.Errorf("embed: wal truncate: %w", err)
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("embed: wal seek after truncate: %w", err)
	}
	w.enc = gob.NewEncoder(w.f)
	return w.f.Sync()
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
