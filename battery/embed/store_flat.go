package embed

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
)

// FlatStore is the default in-memory [Store]: a slice of chunks scored
// by brute-force cosine similarity. It targets up to ~100k chunks; for
// 384-dim float32 vectors that's roughly 150MB plus chunk text, with
// query latency under ~30ms on a modern CPU.
type FlatStore struct {
	mu     sync.RWMutex
	dim    int
	model  string
	chunks []Chunk
	// byDoc indexes chunk positions by DocID for O(removed) deletes.
	byDoc map[string][]int
}

// NewFlatStore returns an empty store sized for vectors of dimension
// dim produced by the named model. dim and model are recorded so
// future persistence (M2) can refuse to load a snapshot embedded with
// a different model.
func NewFlatStore(dim int, model string) *FlatStore {
	return &FlatStore{
		dim:   dim,
		model: model,
		byDoc: make(map[string][]int),
	}
}

// Add appends chunks. Vectors are L2-normalized in place so query-time
// scoring is a plain dot product. When the store was constructed with
// dim=0 (embedder dim unknown at boot time), the first non-empty vec
// fixes the dimension for the lifetime of the store.
func (s *FlatStore) Add(_ context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dim == 0 && len(chunks) > 0 && len(chunks[0].Vec) > 0 {
		s.dim = len(chunks[0].Vec)
	}
	for i := range chunks {
		if len(chunks[i].Vec) != s.dim {
			return errVecDim(s.dim, len(chunks[i].Vec))
		}
		normalize(chunks[i].Vec)
		idx := len(s.chunks)
		s.chunks = append(s.chunks, chunks[i])
		s.byDoc[chunks[i].DocID] = append(s.byDoc[chunks[i].DocID], idx)
	}
	return nil
}

// RemoveDoc deletes every chunk belonging to docID. It performs a
// stable, in-place compaction so retained chunks keep their relative
// order — useful when persistence writes the underlying slice in
// insertion order.
func (s *FlatStore) RemoveDoc(_ context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	positions, ok := s.byDoc[docID]
	if !ok || len(positions) == 0 {
		return nil
	}
	delete(s.byDoc, docID)
	doomed := make(map[int]struct{}, len(positions))
	for _, p := range positions {
		doomed[p] = struct{}{}
	}
	// Rebuild chunks and byDoc in a single pass.
	kept := s.chunks[:0]
	newByDoc := make(map[string][]int, len(s.byDoc))
	for old, c := range s.chunks {
		if _, gone := doomed[old]; gone {
			continue
		}
		newIdx := len(kept)
		kept = append(kept, c)
		newByDoc[c.DocID] = append(newByDoc[c.DocID], newIdx)
	}
	s.chunks = kept
	s.byDoc = newByDoc
	return nil
}

// Candidates returns up to top chunks by cosine similarity to qv,
// applying filter to the candidate set first.
func (s *FlatStore) Candidates(_ context.Context, qv []float32, filter Filter, top int) ([]Hit, error) {
	if s.dim != 0 && len(qv) != s.dim {
		return nil, errVecDim(s.dim, len(qv))
	}
	if top <= 0 {
		top = 10
	}
	q := make([]float32, len(qv))
	copy(q, qv)
	normalize(q)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// We can never return more candidates than chunks exist, so bound the
	// capacity hint by the corpus size. This stops a pathological caller
	// top (e.g. derived from an attacker-supplied K) from pre-allocating
	// gigabytes against a tiny store.
	capHint := top * 2
	if capHint > len(s.chunks) || capHint < 0 {
		capHint = len(s.chunks)
	}
	hits := make([]Hit, 0, capHint)
	for i := range s.chunks {
		c := &s.chunks[i]
		if !chunkMatches(c, filter) {
			continue
		}
		score := dot(q, c.Vec)
		hits = append(hits, Hit{Chunk: *c, Score: float64(score), Reason: "vec"})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Chunk.ID < hits[j].Chunk.ID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > top {
		hits = hits[:top]
	}
	// Vectors are intentionally retained here — downstream retrieval
	// stages (MMR diversity, reranking) need them. The Index strips
	// vectors right before returning to the caller.
	return hits, nil
}

// Stats returns a snapshot of store state.
func (s *FlatStore) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		Docs:   len(s.byDoc),
		Chunks: len(s.chunks),
		Dim:    s.dim,
		Model:  s.model,
	}
}

func chunkMatches(c *Chunk, f Filter) bool {
	if f.Source != "" && c.Source != f.Source {
		return false
	}
	if f.Kind != "" {
		got, _ := c.Metadata["kind"].(string)
		if got != f.Kind {
			return false
		}
	}
	for k, want := range f.MetaMatch {
		if c.Metadata[k] != want {
			return false
		}
	}
	return true
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func errVecDim(want, got int) error {
	return &dimError{want: want, got: got}
}

type dimError struct{ want, got int }

func (e *dimError) Error() string {
	return "embed: vector dimension mismatch (want " + itoa(e.want) + ", got " + itoa(e.got) + ")"
}

func itoa(i int) string {
	// keep store_flat.go free of fmt to minimise allocations in hot paths
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// compile-time assertion that FlatStore implements Store.
var _ Store = (*FlatStore)(nil)

// errEmptyStore is returned by future persistence code when a snapshot
// header is present but the chunk list is empty. Reserved for M2.
var errEmptyStore = errors.New("embed: store snapshot has no chunks")

var _ = errEmptyStore
