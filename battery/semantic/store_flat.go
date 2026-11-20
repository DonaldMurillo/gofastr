package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/deepcopy"
)

type FlatStore struct {
	mu     sync.RWMutex
	dim    int
	model  string
	chunks []Chunk
	byDoc  map[string][]int
}

// NewFlatStore returns an empty FlatStore for vectors of dimension dim from
// model.
func NewFlatStore(dim int, model string) *FlatStore {
	return &FlatStore{
		dim:   dim,
		model: model,
		byDoc: make(map[string][]int),
	}
}

// Add stores copies of validated chunks with normalized vectors, all or none.
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
	if dim == 0 {
		return errEmptyVector
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

// RemoveDoc deletes every chunk of docID, keeping the others' order.
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

// Candidates returns up to top chunks by cosine similarity to qv inside
// filter.
func (s *FlatStore) Candidates(ctx context.Context, qv []float32, filter Filter, top int) ([]Hit, error) {
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

	capHint := top * 2
	if capHint > len(s.chunks) || capHint < 0 {
		capHint = len(s.chunks)
	}
	hits := make([]Hit, 0, capHint)
	match := newFilterMatcher(filter)
	for i := range s.chunks {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		c := &s.chunks[i]
		if !match.matches(c) {
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
	for i := range hits {
		hits[i].Chunk = cloneChunk(hits[i].Chunk)
	}
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

// ChunkIDsForDoc returns the IDs of every chunk of docID.
func (s *FlatStore) ChunkIDsForDoc(docID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	positions := s.byDoc[docID]
	out := make([]string, 0, len(positions))
	for _, p := range positions {
		out = append(out, s.chunks[p].ID)
	}
	return out, nil
}

// ChunkByID returns the chunk with the given ID.
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

// AllChunks returns a copy of every stored chunk.
func (s *FlatStore) AllChunks() []Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Chunk, len(s.chunks))
	for i, c := range s.chunks {
		out[i] = cloneChunk(c)
	}
	return out
}

type filterMatcher struct {
	source, kind string
	wants        []metaWant
}

type metaWant struct {
	key  string
	raw  any
	json any
	ok   bool
}

func newFilterMatcher(f Filter) *filterMatcher {
	m := &filterMatcher{source: f.Source, kind: f.Kind}
	for k, want := range f.MetaMatch {
		w := metaWant{key: k, raw: want}
		if v, ok := asJSONValue(want); ok {
			w.json, w.ok = canonJSON(v), true
		}
		m.wants = append(m.wants, w)
	}
	return m
}

func (m *filterMatcher) matches(c *Chunk) bool {
	if m.source != "" && c.Source != m.source {
		return false
	}
	if m.kind != "" {
		got, _ := c.Metadata["kind"].(string)
		if got != m.kind {
			return false
		}
	}
	for i := range m.wants {
		w := &m.wants[i]
		got, ok := c.Metadata[w.key]
		if !ok || !w.equals(got) {
			return false
		}
	}
	return true
}

func (w *metaWant) equals(got any) bool {
	if reflect.DeepEqual(got, w.raw) {
		return true
	}
	if plainScalar(got) && plainScalar(w.raw) {
		return false
	}
	if !w.ok {
		return false
	}
	g, ok := asJSONValue(got)
	return ok && jsonValueEqual(g, w.json)
}

func asJSONValue(v any) (any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	return out, dec.Decode(&out) == nil
}

type canonNumber struct {
	neg    bool
	digits string
	exp    int64
	ok     bool
}

func canonOf(literal json.Number) canonNumber {
	s := string(literal)
	neg := strings.HasPrefix(s, "-")
	mantissa, expText, hasExp := strings.Cut(strings.ToLower(strings.TrimPrefix(s, "-")), "e")
	var exp int64
	if hasExp {
		parsed, err := strconv.ParseInt(expText, 10, 64)
		if err != nil {
			return canonNumber{}
		}
		exp = parsed
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	exp -= int64(len(fraction))
	trimmed := strings.TrimRight(digits, "0")
	exp += int64(len(digits) - len(trimmed))
	if trimmed == "" {
		return canonNumber{ok: true}
	}
	return canonNumber{neg: neg, digits: trimmed, exp: exp, ok: true}
}

func canonJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		return canonOf(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = canonJSON(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = canonJSON(e)
		}
		return out
	}
	return v
}

func jsonValueEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(canonNumber)
		return ok && bv.ok && canonOf(av) == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonValueEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, x := range av {
			y, ok := bv[k]
			if !ok || !jsonValueEqual(x, y) {
				return false
			}
		}
		return true
	}
	return a == b
}

func plainScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool:
		return true
	}
	return false
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

var _ Store = (*FlatStore)(nil)

var errEmptyStore = errors.New("semantic: store snapshot has no chunks")

var _ = errEmptyStore

func cloneChunk(c Chunk) Chunk {
	c.Vec = slices.Clone(c.Vec)
	if c.Metadata != nil {
		c.Metadata = deepcopy.Map(c.Metadata)
	}
	return c
}

var errEmptyVector = errors.New("semantic: vector is empty")

func finiteVector(v []float32) error {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return errors.New("semantic: vector coordinates must be finite")
		}
	}
	return nil
}
