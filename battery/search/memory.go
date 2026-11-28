package search

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/internal/deepcopy"
)

// Memory is an in-memory search backend suitable for tests and small
// examples.
type Memory struct {
	mu   sync.RWMutex
	docs map[string]Document

	closeOnce       sync.Once
	unregisterErase func()
}

// NewMemory creates an empty in-memory search index and registers its
// store eraser.
func NewMemory() *Memory {
	m := &Memory{docs: make(map[string]Document)}
	_, m.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "search:memory",
		Source: "search",
		Erase:  m.eraseOwner,
	})
	return m
}

// Close unregisters the index's store eraser, so an erasure no longer
// reaches it.
func (m *Memory) Close() error {
	m.closeOnce.Do(m.unregisterErase)
	return nil
}

func (m *Memory) eraseOwner(ctx context.Context, owner string, dryRun bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if owner == "" {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, doc := range m.docs {
		if doc.Owner != owner {
			continue
		}
		n++
		if !dryRun {
			delete(m.docs, id)
		}
	}
	return n, nil
}

// Index inserts or replaces a document.
func (m *Memory) Index(ctx context.Context, doc Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := marshalFields(doc.Fields); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	doc.Fields = deepcopy.Value(doc.Fields).(map[string]any)
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

const maxQueryTerms = 64

// Search returns documents whose text or string fields hold every query
// term as a word, the last term as the start of a word: the terms and the
// words split the way the SQL backends split them (queryTerms), with no
// stemming.
func (m *Memory) Search(ctx context.Context, query Query) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateFieldEquals(query.FieldEquals); err != nil {
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
	offset := max(query.Offset, 0)
	if offset >= len(results) {
		return nil, nil
	}
	end := len(results)
	if query.Limit > 0 {
		if sum := offset + query.Limit; sum >= offset && sum < end {
			end = sum
		}
	}
	page := append([]Result(nil), results[offset:end]...)
	for i := range page {
		page[i].Document.Fields = deepcopy.Value(page[i].Document.Fields).(map[string]any)
	}
	return page, nil
}

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
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if s, ok := fields[k].(string); ok {
			sb.WriteByte(' ')
			sb.WriteString(s)
		}
	}
	return sb.String()
}

func matchesFields(fields map[string]any, want map[string]string) bool {
	for k, v := range want {
		got, ok := fields[k].(string)
		if !ok || got != v {
			return false
		}
	}
	return true
}
