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

const snapshotVersion = 1

type snapshotHeader struct {
	Version    int
	Model      string
	Dim        int
	ChunkCount int
	CreatedAt  int64
}

// Snapshot atomically writes the store to path.
func (s *FlatStore) Snapshot(path string) error {
	if path == "" {
		return errors.New("semantic: Snapshot path is required")
	}
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

// dimension, a [*ModelMismatchError] is returned and the store is left
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

// ModelMismatchError is returned when stored vectors came from another
// embedder model or dimension.
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

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register(json.Number(""))
}

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

type walOp string

const (
	walOpAdd     walOp = "add"
	walOpRemove  walOp = "remove"
	walOpReplace walOp = "replace"
)

type walEntry struct {
	Op     walOp
	DocID  string
	DocIDs []string
	Chunks []Chunk
	Model  string
}

type wal struct {
	mu      sync.Mutex
	path    string
	f       *os.File
	enc     *gob.Encoder
	writeFn func(f *os.File, p []byte) (int, error)
	syncFn  func(f *os.File) error
	broken  error
}

type walSink struct{ w *wal }

func (s walSink) Write(p []byte) (int, error) {
	if s.w.writeFn != nil {
		return s.w.writeFn(s.w.f, p)
	}
	return s.w.f.Write(p)
}

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

func (w *wal) refuse(size int64, err error) {
	w.broken = err
	_ = os.Truncate(w.path, size)
	_ = w.f.Sync()
}

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
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("semantic: wal seek end: %w", err)
	}
	w.enc = gob.NewEncoder(walSink{w})
	return nil
}

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

func (w *wal) truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("semantic: WAL is closed")
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("semantic: wal sync before truncate: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("semantic: wal close before truncate: %w", err)
	}
	w.f = nil
	f, err := os.OpenFile(w.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		w.f, _ = os.OpenFile(w.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
		if w.f != nil {
			w.enc = gob.NewEncoder(walSink{w})
		}
		return fmt.Errorf("semantic: wal reopen after truncate: %w", err)
	}
	w.f = f
	w.enc = gob.NewEncoder(walSink{w})
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
