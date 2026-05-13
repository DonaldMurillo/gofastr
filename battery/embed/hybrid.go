package embed

import (
	"context"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// rrfK is the conventional reciprocal-rank-fusion constant. 60 dampens
// the contribution of low-ranked items from either list and is
// effectively the published default — varying it within ±20 doesn't
// change top-K composition meaningfully for our use case.
const rrfK = 60.0

// fuseRRF merges vector and keyword candidate lists using reciprocal
// rank fusion. Both inputs MUST be sorted by score in descending
// order. The output is sorted by fused score in descending order and
// each hit's Reason is set to "hybrid".
func fuseRRF(vec, kw []Hit) []Hit {
	scores := make(map[string]float64, len(vec)+len(kw))
	chunks := make(map[string]Hit, len(vec)+len(kw))
	for rank, h := range vec {
		scores[h.Chunk.ID] += 1.0 / (rrfK + float64(rank+1))
		chunks[h.Chunk.ID] = h
	}
	for rank, h := range kw {
		scores[h.Chunk.ID] += 1.0 / (rrfK + float64(rank+1))
		// Only set chunks[id] if not already present from the vector
		// pass — the vector pass carries the embedding, which MMR
		// needs.
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

// MemoryKeyword is an allocation-cheap, dependency-free
// [KeywordBackend] used as the default for [Options.Keyword] when the
// caller wants hybrid retrieval without wiring battery/search.
//
// It scores documents using a simplified BM25-flavoured formula: term
// frequency in the document, normalised by document length and scaled
// by inverse document frequency. This is not BM25 — there is no IDF
// saturation parameter — but it is the right shape for fusing with
// vector scores via RRF, where only the rank order matters.
type MemoryKeyword struct {
	mu     sync.RWMutex
	docs   map[string]keywordDoc
	totals map[string]int // term -> # of docs containing term (for IDF)
}

type keywordDoc struct {
	id     string
	length int            // total token count
	tf     map[string]int // term frequency
}

// NewMemoryKeyword returns an empty in-memory keyword backend.
func NewMemoryKeyword() *MemoryKeyword {
	return &MemoryKeyword{
		docs:   make(map[string]keywordDoc),
		totals: make(map[string]int),
	}
}

// Index implements [KeywordBackend]. Re-indexing the same id replaces
// the prior entry so totals stay consistent.
func (m *MemoryKeyword) Index(_ context.Context, id, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prior, ok := m.docs[id]; ok {
		for term := range prior.tf {
			m.totals[term]--
			if m.totals[term] <= 0 {
				delete(m.totals, term)
			}
		}
	}
	tokens := keywordTokens(text)
	tf := make(map[string]int, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	m.docs[id] = keywordDoc{id: id, length: len(tokens), tf: tf}
	for term := range tf {
		m.totals[term]++
	}
	return nil
}

// Delete implements [KeywordBackend].
func (m *MemoryKeyword) Delete(_ context.Context, id string) error {
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
	delete(m.docs, id)
	return nil
}

// Search implements [KeywordBackend]. Documents with zero matching
// terms are not returned.
func (m *MemoryKeyword) Search(_ context.Context, text string, top int) ([]KeywordHit, error) {
	if top <= 0 {
		top = 10
	}
	terms := keywordTokens(text)
	if len(terms) == 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.docs) == 0 {
		return nil, nil
	}
	totalDocs := float64(len(m.docs))
	type scored struct {
		id    string
		score float64
	}
	hits := make([]scored, 0, len(m.docs))
	for id, doc := range m.docs {
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
				idf = 0.0001 // tiny floor so common terms still contribute order
			}
			// Length-normalised TF: dampens long-document advantage.
			tfNorm := float64(tf) / (float64(tf) + 0.5 + 1.5*float64(doc.length)/avgLen(m))
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
	var sum int
	for _, d := range m.docs {
		sum += d.length
	}
	avg := float64(sum) / float64(len(m.docs))
	if avg < 1 {
		return 1
	}
	return avg
}

func keywordTokens(s string) []string {
	lower := strings.ToLower(s)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// logBase is a tiny natural-log-ish helper that avoids math.Log on the
// hot path while preserving monotonicity. Strict accuracy is not
// needed — fusion only uses rank, not magnitude.
func logBase(x float64) float64 {
	if x <= 1 {
		return 0
	}
	// 6-term Taylor-ish approximation of ln(x) for x > 1.
	// ln(x) = 2 * artanh((x-1)/(x+1))
	y := (x - 1) / (x + 1)
	y2 := y * y
	return 2 * y * (1 + y2/3 + y2*y2/5)
}

var _ KeywordBackend = (*MemoryKeyword)(nil)
