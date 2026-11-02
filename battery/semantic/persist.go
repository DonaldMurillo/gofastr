package semantic

import (
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
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
	if err := fileperm.MkdirPrivate(filepath.Dir(path)); err != nil {
		return fmt.Errorf("semantic: mkdir snapshot dir: %w", err)
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
	if header.ChunkCount > 0 && header.Dim == 0 {
		return errEmptyVector
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

// ModelMismatchError is returned when stored vectors (a persisted
// snapshot or WAL, or a PgVectorStore table's recorded model) were
// produced by a different embedder than the one configured. Mixing
// vectors from different models is silently catastrophic for retrieval
// quality, so the open is refused loudly instead. The Snapshot fields
// name what is stored.
type ModelMismatchError struct {
	SnapshotModel, StoreModel string
	SnapshotDim, StoreDim     int
}

func (e *ModelMismatchError) Error() string {
	return fmt.Sprintf(
		"semantic: stored vectors were embedded with model=%q dim=%d, the index is configured with model=%q dim=%d: re-index after model changes",
		e.SnapshotModel, e.SnapshotDim, e.StoreModel, e.StoreDim,
	)
}

// Metadata arriving as JSON (the /add route, a watcher callback that
// decodes a config) holds nested objects and arrays. gob carries a value
// inside an interface only when its concrete type is registered, so
// without these the WAL and the snapshot refuse every nested value.
// The HTTP routes decode numbers as json.Number so a large integer keeps
// its digits, which makes json.Number a value the WAL must carry too.
func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(json.Number(""))
}

// walEncodable reports whether the WAL can encode chunks. Only Metadata
// varies in type (the other Chunk fields are fixed), so only it is
// encoded, into a throwaway stream.
func walEncodable(chunks []Chunk) error {
	enc := gob.NewEncoder(io.Discard)
	for j := range chunks {
		if len(chunks[j].Metadata) == 0 {
			continue
		}
		if err := enc.Encode(chunks[j].Metadata); err != nil {
			return fmt.Errorf("semantic: chunk %q metadata cannot be persisted: %w", chunks[j].ID, err)
		}
	}
	return nil
}

// walOp is the discriminator for [walEntry].
type walOp string

const (
	// walOpAdd and walOpRemove are no longer written: a WAL from an
	// earlier release holds them, and replay still applies them.
	walOpAdd    walOp = "add"
	walOpRemove walOp = "remove"
	// walOpReplace removes every document in DocIDs, then stores Chunks
	// (none for a plain Remove): one record, so a crash replays all of a
	// replace or none of it.
	walOpReplace walOp = "replace"
)

// walEntry is one record in the write-ahead log. Chunks carry their
// vectors so replay does not need to call the embedder, and Model names
// the embedder, so replay refuses vectors from another model the way
// LoadSnapshot does (an entry written before the field existed has none
// and replays unchecked). Deletion is always doc-scoped: DocIDs for a
// replace, DocID for a legacy remove.
type walEntry struct {
	Op     walOp
	DocID  string
	DocIDs []string
	Chunks []Chunk
	Model  string
}

// wal is a tiny append-only write-ahead log. It is not high-performance.
// One fsync per write, but it is correct and easy to reason about,
// and at 100k chunks the snapshot path dominates throughput anyway.
type wal struct {
	mu   sync.Mutex
	path string
	f    *os.File
	enc  *gob.Encoder
	// writeFn and syncFn, when set, replace the file write under the
	// encoder and an append's fsync: a test injects a disk that fails
	// partway through a record, or after it.
	writeFn func(f *os.File, p []byte) (int, error)
	syncFn  func(f *os.File) error
	// broken is the failed append that stopped the WAL; see refuse.
	broken error
}

// walSink is the encoder's writer: the WAL's current file, through
// writeFn when one is set.
type walSink struct{ w *wal }

func (s walSink) Write(p []byte) (int, error) {
	if s.w.writeFn != nil {
		return s.w.writeFn(s.w.f, p)
	}
	return s.w.f.Write(p)
}

// sync is an append's fsync, through syncFn when one is set.
func (s walSink) sync() error {
	if s.w.syncFn != nil {
		//gofastr:allow(recovercallback) test-only seam, unexported and nil outside this package's tests
		return s.w.syncFn(s.w.f)
	}
	return s.w.f.Sync()
}

func openWAL(path string) (*wal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("semantic: mkdir wal dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("semantic: open wal: %w", err)
	}
	// O_CREATE's 0600 applies only to a new file; tighten the handle so
	// a pre-existing looser WAL is owner-only too.
	if err := fileperm.RestrictOpenFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("semantic: restrict wal: %w", err)
	}
	w := &wal{path: path, f: f}
	w.enc = gob.NewEncoder(walSink{w})
	return w, nil
}

func (w *wal) append(e walEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("semantic: WAL is closed")
	}
	if w.broken != nil {
		return fmt.Errorf("semantic: WAL takes no appends until a snapshot rewrites it: %w", w.broken)
	}
	st, err := w.f.Stat()
	if err != nil {
		return fmt.Errorf("semantic: wal stat: %w", err)
	}
	if err := w.enc.Encode(e); err != nil {
		w.refuse(st.Size(), err)
		return fmt.Errorf("semantic: wal append: %w", err)
	}
	if err := (walSink{w}).sync(); err != nil {
		w.refuse(st.Size(), err)
		return fmt.Errorf("semantic: wal sync: %w", err)
	}
	return nil
}

// refuse handles an append that failed partway (a full disk). The torn
// bytes go, so the file holds exactly the acknowledged records, and the
// WAL takes no more appends: the encoder believes it sent type
// descriptors the file may have lost, and a fresh encoder resends them
// mid-stream, which the replay decoder refuses as a duplicate type. A
// record written after a torn one was read as its payload, and every
// restart failed. truncate (after a snapshot) starts a clean stream. If
// the cut fails too, the torn bytes stay at the tail, which replay
// already heals.
func (w *wal) refuse(size int64, err error) {
	w.broken = err
	_ = os.Truncate(w.path, size)
	_ = w.f.Sync()
}

// needsSnapshot reports whether a failed append has stopped the WAL
// until a snapshot rewrites it.
func (w *wal) needsSnapshot() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.broken != nil
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
	if err := decodeWAL(w.f, apply); err != nil {
		return err
	}
	// Seek to end so further appends go after the replayed entries.
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("semantic: wal seek end: %w", err)
	}
	w.enc = gob.NewEncoder(walSink{w})
	return nil
}

// decodeWAL applies every whole record of f from its current offset and
// stops at a record cut short at the end: a crash mid-append, or a
// writer's append still in flight. The writer's Open never appends after
// such a tail: it snapshots the replayed state and starts an empty WAL.
func decodeWAL(f *os.File, apply func(walEntry) error) error {
	dec := gob.NewDecoder(f)
	for {
		var e walEntry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("semantic: wal decode: %w", err)
		}
		if err := apply(e); err != nil {
			return fmt.Errorf("semantic: wal apply: %w", err)
		}
	}
}

// replayReadOnly applies every whole record of the WAL at path, opened
// for reading only: a writer may be appending to it, so a torn tail is
// its record in flight and stays. A missing WAL is an empty one.
func replayReadOnly(path string, apply func(walEntry) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("semantic: open wal: %w", err)
	}
	defer f.Close()
	return decodeWAL(f, apply)
}

// lockIndexDir takes the writer's exclusive lock on dir, creating it,
// and returns the lock file, whose Close releases it. Another holder is
// ErrIndexLocked.
func lockIndexDir(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("semantic: mkdir index dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("semantic: open index lock: %w", err)
	}
	held, err := filelock.TryLock(f)
	if err == nil && !held {
		err = ErrIndexLocked
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
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
			w.enc = gob.NewEncoder(walSink{w})
		}
		return fmt.Errorf("semantic: wal reopen after truncate: %w", err)
	}
	w.f = f
	w.enc = gob.NewEncoder(walSink{w})
	// An empty file and a fresh encoder: the stream a failed append
	// broke starts over.
	w.broken = nil
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
