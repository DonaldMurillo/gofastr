package semantic

import (
	"context"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const rrfK = 60.0

func fuseRRF(vec, kw []Hit) []Hit {
	scores := make(map[string]float64, len(vec)+len(kw))
	chunks := make(map[string]Hit, len(vec)+len(kw))
	for rank, h := range vec {
		scores[h.Chunk.ID] += 1.0 / (rrfK + float64(rank+1))
		chunks[h.Chunk.ID] = h
	}
	for rank, h := range kw {
		scores[h.Chunk.ID] += 1.0 / (rrfK + float64(rank+1))
		if _, ok := chunks[h.Chunk.ID]; !ok {
			chunks[h.Chunk.ID] = h
		}
	}
	out := make([]Hit, 0, len(scores))
	for id, s := range scores {
		h := chunks[id]
		h.Score = s
		h.Reason = "hybrid"
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Chunk.ID < out[j].Chunk.ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// MemoryKeyword is a dependency-free in-process KeywordBackend scored with
// BM25-style weights.
type MemoryKeyword struct {
	mu       sync.RWMutex
	docs     map[string]keywordDoc
	totals   map[string]int
	totalLen int
}

type keywordDoc struct {
	id     string
	length int
	tf     map[string]int
}

// NewMemoryKeyword returns an empty in-memory keyword backend.
func NewMemoryKeyword() *MemoryKeyword {
	return &MemoryKeyword{
		docs:   make(map[string]keywordDoc),
		totals: make(map[string]int),
	}
}

// Index implements KeywordBackend; re-indexing an id replaces its entry.
func (m *MemoryKeyword) Index(ctx context.Context, id, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if prior, ok := m.docs[id]; ok {
		for term := range prior.tf {
			m.totals[term]--
			if m.totals[term] <= 0 {
				delete(m.totals, term)
			}
		}
		m.totalLen -= prior.length
	}
	tokens := keywordTokens(text)
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	m.docs[id] = keywordDoc{id: id, length: len(tokens), tf: tf}
	m.totalLen += len(tokens)
	for term := range tf {
		m.totals[term]++
	}
	return nil
}

// Delete implements [KeywordBackend].
func (m *MemoryKeyword) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[id]
	if !ok {
		return nil
	}
	for term := range doc.tf {
		m.totals[term]--
		if m.totals[term] <= 0 {
			delete(m.totals, term)
		}
	}
	m.totalLen -= doc.length
	delete(m.docs, id)
	return nil
}

// Search implements KeywordBackend; a document with no matching term is not
// returned.
func (m *MemoryKeyword) Search(ctx context.Context, text string, top int) ([]KeywordHit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if top <= 0 {
		top = 10
	}
	terms := keywordTokens(text)
	seen := make(map[string]bool, len(terms))
	unique := terms[:0]
	for _, term := range terms {
		if !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}
	terms = unique
	if len(terms) == 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.docs) == 0 {
		return nil, nil
	}
	totalDocs := float64(len(m.docs))
	avg := avgLen(m)
	type scored struct {
		id    string
		score float64
	}
	hits := make([]scored, 0, len(m.docs))
	for id, doc := range m.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var s float64
		for _, term := range terms {
			tf := doc.tf[term]
			if tf == 0 {
				continue
			}
			df := m.totals[term]
			if df == 0 {
				continue
			}
			idf := logBase(totalDocs / float64(df))
			if idf <= 0 {
				idf = 0.0001
			}
			tfNorm := float64(tf) / (float64(tf) + 0.5 + 1.5*float64(doc.length)/avg)
			s += idf * tfNorm
		}
		if s > 0 {
			hits = append(hits, scored{id: id, score: s})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].id < hits[j].id
		}
		return hits[i].score > hits[j].score
	})
	if len(hits) > top {
		hits = hits[:top]
	}
	out := make([]KeywordHit, len(hits))
	for i, h := range hits {
		out[i] = KeywordHit{ChunkID: h.id, Score: h.score}
	}
	return out, nil
}

func avgLen(m *MemoryKeyword) float64 {
	if len(m.docs) == 0 {
		return 1
	}
	avg := float64(m.totalLen) / float64(len(m.docs))
	if avg < 1 {
		return 1
	}
	return avg
}

func keywordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func keywordTokens(s string) []string {
	lower := strings.ToLower(s)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !keywordRune(r)
	})
}

func logBase(x float64) float64 {
	if x <= 1 {
		return 0
	}
	y := (x - 1) / (x + 1)
	y2 := y * y
	return 2 * y * (1 + y2/3 + y2*y2/5)
}

var _ KeywordBackend = (*MemoryKeyword)(nil)
