package search

import (
	"context"
	"maps"
	"slices"
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
func (m *Memory) Index(ctx context.Context, doc Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc.Fields = maps.Clone(doc.Fields)
	m.docs[doc.ID] = doc
	return nil
}

// Delete removes a document by ID.
func (m *Memory) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, id)
	return nil
}

// maxQueryTerms bounds how many distinct terms a single query may contribute
// to scoring. Without a cap, an attacker-controlled query string of many
// tokens forces O(terms x corpus x doclen) word scans per request. Distinct
// terms beyond this cap add negligible selectivity.
const maxQueryTerms = 64

// Search returns documents whose text or string fields hold every query
// term as a word, the last term as the start of a word: the terms and
// the words split the way the SQL backends split them (queryTerms), with
// no stemming. Empty text matches every document; text whose terms all
// sanitize away (pure punctuation) matches none.
func (m *Memory) Search(ctx context.Context, query Query) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	terms := queryTerms(query.Text)
	if len(terms) == 0 && len(strings.Fields(query.Text)) > 0 {
		return nil, nil
	}
	var results []Result
	for _, doc := range m.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if query.Type != "" && doc.Type != query.Type {
			continue
		}
		if !matchesFields(doc.Fields, query.FieldEquals) {
			continue
		}
		score := scoreTerms(docWords(doc.Text+" "+fieldsText(doc.Fields)), terms)
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
	offset := max(query.Offset, 0)
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
	// Copy Fields for the returned page only, still under the read
	// lock: the caller owns them, the rest never leave.
	page := append([]Result(nil), results[offset:end]...)
	for i := range page {
		page[i].Document.Fields = maps.Clone(page[i].Document.Fields)
	}
	return page, nil
}

// docWords splits text into the words queryTerms reads from a query, in
// order with repeats. A word joined by - or _ also yields its parts, as
// the SQL backends' tokenizers index them, so foo matches foo-bar.
func docWords(text string) []string {
	var words []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return !termRune(r) }) {
		w := sanitizeTsTerm(part)
		words = append(words, w)
		if strings.ContainsAny(w, "-_") {
			for _, sub := range strings.FieldsFunc(w, func(r rune) bool { return r == '-' || r == '_' }) {
				words = append(words, sub)
			}
		}
	}
	return words
}

// scoreTerms counts, per term, the words it matches: the whole word, or
// for the last term any word it starts. A term matching no word scores
// the document 0.
func scoreTerms(words, terms []string) float64 {
	if len(terms) == 0 {
		return 1
	}
	var score float64
	for i, term := range terms {
		last := i == len(terms)-1
		count := 0
		for _, w := range words {
			if w == term || (last && strings.HasPrefix(w, term)) {
				count++
			}
		}
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
	// Sorted so the searchable blob is stable: an unordered concat makes
	// cross-field phrase matches come and go between calls.
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if s, ok := fields[k].(string); ok {
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
