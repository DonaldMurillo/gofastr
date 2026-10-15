package semantic

import (
	"context"
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
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

// Add appends validated chunks, keeping its own vectors and metadata map.
// Vectors are L2-normalized so query-time
// scoring is a plain dot product. When the store was constructed with
// dim=0 (embedder dim unknown at boot time), the first non-empty vec
// fixes the dimension for the lifetime of the store.
func (s *FlatStore) Add(_ context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dim := s.dim
	if dim == 0 && len(chunks[0].Vec) > 0 {
		dim = len(chunks[0].Vec)
	}
	for _, c := range chunks {
		if len(c.Vec) != dim {
			return errVecDim(dim, len(c.Vec))
		}
		if err := finiteVector(c.Vec); err != nil {
			return err
		}
	}
	s.dim = dim
	for _, c := range chunks {
		c = cloneChunk(c)
		normalize(c.Vec)
		idx := len(s.chunks)
		s.chunks = append(s.chunks, c)
		s.byDoc[c.DocID] = append(s.byDoc[c.DocID], idx)
	}
	return nil
}

// RemoveDoc deletes every chunk belonging to docID. It performs a
// stable, in-place compaction so retained chunks keep their relative
// order, useful when persistence writes the underlying slice in
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
	clear(s.chunks[len(kept):])
	s.chunks = kept
	s.byDoc = newByDoc
	return nil
}

// Candidates returns up to top chunks by cosine similarity to qv,
// applying filter to the candidate set first.
func (s *FlatStore) Candidates(_ context.Context, qv []float32, filter Filter, top int) ([]Hit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := finiteVector(qv); err != nil {
		return nil, err
	}
	if s.dim != 0 && len(qv) != s.dim {
		return nil, errVecDim(s.dim, len(qv))
	}
	if top <= 0 {
		top = 10
	}
	q := make([]float32, len(qv))
	copy(q, qv)
	normalize(q)

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
		hits = append(hits, Hit{Chunk: cloneChunk(*c), Score: float64(score), Reason: "vec"})
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
	// Vectors are intentionally retained here, downstream retrieval
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

// ChunkIDsForDoc returns the IDs of every chunk belonging to docID. Part of
// the chunkLister capability the hybrid/keyword index uses to purge a doc's
// keyword entries when the doc is removed.
func (s *FlatStore) ChunkIDsForDoc(docID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	positions := s.byDoc[docID]
	out := make([]string, 0, len(positions))
	for _, p := range positions {
		out = append(out, s.chunks[p].ID)
	}
	return out
}

// ChunkByID returns the chunk with the given ID. Part of chunkLister; used to
// hydrate keyword-only hits (which carry just a chunk ID) into full Hits.
func (s *FlatStore) ChunkByID(id string) (Chunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.chunks {
		if s.chunks[i].ID == id {
			return cloneChunk(s.chunks[i]), true
		}
	}
	return Chunk{}, false
}

// AllChunks returns a copy of every stored chunk. Part of chunkLister; used to
// rebuild the keyword index after a snapshot load (snapshots don't persist
// keyword state).
func (s *FlatStore) AllChunks() []Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Chunk, len(s.chunks))
	for i, c := range s.chunks {
		out[i] = cloneChunk(c)
	}
	return out
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
		if !reflect.DeepEqual(c.Metadata[k], want) {
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
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
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
	return "semantic: vector dimension mismatch (want " + strconv.Itoa(e.want) + ", got " + strconv.Itoa(e.got) + ")"
}

// compile-time assertion that FlatStore implements Store.
var _ Store = (*FlatStore)(nil)

// errEmptyStore is returned by future persistence code when a snapshot
// header is present but the chunk list is empty. Reserved for M2.
var errEmptyStore = errors.New("semantic: store snapshot has no chunks")

var _ = errEmptyStore

// cloneChunk isolates vectors and the top-level metadata map at the store boundary.
func cloneChunk(c Chunk) Chunk {
	c.Vec = slices.Clone(c.Vec)
	c.Metadata = maps.Clone(c.Metadata)
	return c
}

func finiteVector(v []float32) error {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return errors.New("semantic: vector coordinates must be finite")
		}
	}
	return nil
}
