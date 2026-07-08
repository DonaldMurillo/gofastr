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

// maxQueryTerms bounds how many distinct terms a single query may contribute
// to scoring. Without a cap, an attacker-controlled query string of many
// whitespace-separated tokens forces O(terms x corpus x doclen) substring
// scans per request. Distinct terms beyond this cap add negligible selectivity.
const maxQueryTerms = 64

// Search returns documents whose text or string fields contain all query terms.
func (m *Memory) Search(_ context.Context, query Query) ([]Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	terms := normalizeTerms(query.Text)
	var results []Result
	for _, doc := range m.docs {
		if query.Type != "" && doc.Type != query.Type {
			continue
		}
		if !matchesFields(doc.Fields, query.FieldEquals) {
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
	// Clamp pagination bounds before slicing. Query is a public type whose
	// Offset/Limit fields may carry any int (e.g. a host mapping raw ?offset=
	// /?limit= params), so guard against negatives and integer overflow.
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(results) {
		return nil, nil
	}
	end := len(results)
	if query.Limit > 0 {
		// offset+query.Limit can overflow to a negative; the `>= offset`
		// check rejects the overflowed case so we never slice past the cap.
		if sum := offset + query.Limit; sum >= offset && sum < end {
			end = sum
		}
	}
	return append([]Result(nil), results[offset:end]...), nil
}

// normalizeTerms lowercases, splits, dedups, and caps the query into a bounded
// set of distinct terms so per-query work cannot be amplified by an
// attacker-controlled query string.
func normalizeTerms(text string) []string {
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		terms = append(terms, f)
		if len(terms) >= maxQueryTerms {
			break
		}
	}
	return terms
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

// matchesFields reports whether fields contains every key in want with a
// string value equal to the wanted value. See Query.FieldEquals for the
// string-only matching rule shared with the Postgres backend.
func matchesFields(fields map[string]any, want map[string]string) bool {
	for k, v := range want {
		got, ok := fields[k].(string)
		if !ok || got != v {
			return false
		}
	}
	return true
}
