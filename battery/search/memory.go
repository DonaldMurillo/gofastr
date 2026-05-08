package search

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Memory is an in-memory search backend suitable for tests and small examples.
type Memory struct {
	mu   sync.RWMutex
	docs map[string]Document
}

// NewMemory creates an empty in-memory search index.
func NewMemory() *Memory {
	return &Memory{docs: make(map[string]Document)}
}

// Index inserts or replaces a document.
func (m *Memory) Index(_ context.Context, doc Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = doc
	return nil
}

// Delete removes a document by ID.
func (m *Memory) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, id)
	return nil
}

// Search returns documents whose text or string fields contain all query terms.
func (m *Memory) Search(_ context.Context, query Query) ([]Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	terms := strings.Fields(strings.ToLower(query.Text))
	var results []Result
	for _, doc := range m.docs {
		if query.Type != "" && doc.Type != query.Type {
			continue
		}
		haystack := strings.ToLower(doc.Text + " " + fieldsText(doc.Fields))
		score := scoreTerms(haystack, terms)
		if score == 0 && len(terms) > 0 {
			continue
		}
		results = append(results, Result{Document: doc, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Document.ID < results[j].Document.ID
		}
		return results[i].Score > results[j].Score
	})
	offset := query.Offset
	if offset > len(results) {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 || offset+limit > len(results) {
		limit = len(results) - offset
	}
	return append([]Result(nil), results[offset:offset+limit]...), nil
}

func scoreTerms(haystack string, terms []string) float64 {
	if len(terms) == 0 {
		return 1
	}
	var score float64
	for _, term := range terms {
		count := strings.Count(haystack, term)
		if count == 0 {
			return 0
		}
		score += float64(count)
	}
	return score
}

func fieldsText(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, value := range fields {
		if s, ok := value.(string); ok {
			sb.WriteByte(' ')
			sb.WriteString(s)
		}
	}
	return sb.String()
}
