package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func postQuery(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A filter number with a huge exponent once cost a big.Rat power per chunk.
func TestQueryMetaMatchHugeExponentIsCheap(t *testing.T) {
	idx, _ := Open(Options{Embedder: NewStubEmbedder(16)})
	var docs []Document
	for i := range 2000 {
		docs = append(docs, Document{ID: fmt.Sprint("d", i), Text: "hello world", Metadata: map[string]any{"n": json.Number("1e131000")}})
	}
	if err := idx.Add(context.Background(), docs...); err != nil {
		t.Fatal(err)
	}
	h := Handler(idx, WithInsecureDisabledAuth())
	start := time.Now()
	for body, want := range map[string]int{
		`{"text":"hello","filter":{"meta_match":{"n":1e999999}}}`:  http.StatusBadRequest,
		`{"text":"hello","filter":{"meta_match":{"n":1e-999999}}}`: http.StatusBadRequest,
		`{"text":"hello","filter":{"meta_match":{"n":1e131071}}}`:  http.StatusOK,
		`{"text":"hello","filter":{"meta_match":{"n":7}}}`:         http.StatusOK,
	} {
		if rec := postQuery(h, body); rec.Code != want {
			t.Errorf("%s: status %d, want %d (%s)", body, rec.Code, want, rec.Body)
		}
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("filtered queries over 2000 chunks took %v", took)
	}
}

func TestMetaMatchNumbersCompareByValue(t *testing.T) {
	c := Chunk{Metadata: map[string]any{"n": json.Number("50.0e-1"), "z": 0, "big": json.Number("9007199254740993")}}
	for _, tc := range []struct {
		key  string
		want any
		ok   bool
	}{
		{"n", 5, true}, {"n", json.Number("0.5e1"), true}, {"n", json.Number("-5"), false}, {"n", 5.5, false},
		{"z", json.Number("-0.00e7"), true}, {"z", 1, false},
		{"big", json.Number("9007199254740993"), true}, {"big", json.Number("9007199254740992"), false},
		{"n", json.Number("1e99999999999999999999"), false},
	} {
		if got := chunkMatches(&c, Filter{MetaMatch: map[string]any{tc.key: tc.want}}); got != tc.ok {
			t.Errorf("%s = %v: matched %v, want %v", tc.key, tc.want, got, tc.ok)
		}
	}
}

func TestFlatStoreCandidatesHonorsCancel(t *testing.T) {
	s := NewFlatStore(2, "m")
	if err := s.Add(context.Background(), []Chunk{{ID: "a", DocID: "a", Vec: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Candidates(ctx, []float32{1, 0}, Filter{}, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// Caller faults on /query answer 400, as they do on /index.
func TestQueryCallerFaultsAnswer400(t *testing.T) {
	idx, _ := Open(Options{Embedder: NewStubEmbedder(16)})
	_ = idx.Add(context.Background(), Document{ID: "a", Text: "hello world"})
	h := Handler(idx, WithInsecureDisabledAuth())
	for _, body := range []string{
		`{"text":"hello","mmr_lambda":2}`,
		`{"text":"hello","rerank":true}`,
		`{"text":"hel\u0000lo"}`,
		`{"text":"hello","filter":{"source":"a\u0000"}}`,
		`{"text":"hello","filter":{"kind":"a\u0000"}}`,
		`{"text":"hello","filter":{"meta_match":{"k":"a\u0000"}}}`,
		`{"text":"hello","filter":{"meta_match":{"k":1e200000}}}`,
	} {
		rec := postQuery(h, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"query_invalid"`) {
			t.Errorf("%s: %d %s, want 400 query_invalid", body, rec.Code, rec.Body)
		}
	}
	var invalid *InvalidQueryError
	if _, err := idx.Query(context.Background(), Query{Text: "x", MMRLambda: 2}); !errors.As(err, &invalid) {
		t.Fatalf("Query err = %v, want *InvalidQueryError", err)
	}
}

type panicReranker struct{}

func (panicReranker) Rerank(context.Context, string, []Hit) ([]Hit, error) { panic("reranker boom") }

type panicOnQueryEmbedder struct{ *StubEmbedder }

func (p panicOnQueryEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 1 && texts[0] == "boom" {
		panic("embedder boom")
	}
	return p.StubEmbedder.Embed(ctx, texts)
}

// Query recovers a panicking backend as Add does.
func TestQueryRecoversBackendPanics(t *testing.T) {
	idx, _ := Open(Options{Embedder: panicOnQueryEmbedder{NewStubEmbedder(16)}, Reranker: panicReranker{}})
	if err := idx.Add(context.Background(), Document{ID: "a", Text: "hello world"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []Query{{Text: "boom"}, {Text: "hello", Rerank: true}} {
		hits, err := idx.Query(context.Background(), q)
		if err == nil || !strings.Contains(err.Error(), "panicked") || hits != nil {
			t.Errorf("Query(%+v) = %v, %v; want a panic turned into an error", q, hits, err)
		}
	}
}

// A directory deleted mid-walk is skipped like a deleted file.
func TestWatcherSkipsDirectoryDeletedMidWalk(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "a.txt"), "alpha")
	mustWrite(filepath.Join(root, "zdir", "b.txt"), "beta")
	idx, _ := Open(Options{Embedder: NewStubEmbedder(16)})
	w := NewWatcher(idx, WatchOptions{PollInterval: -1, MetadataFunc: func(string) map[string]any {
		_ = os.RemoveAll(filepath.Join(root, "zdir")) // another process, mid-walk
		return nil
	}})
	if err := w.ScanOnce(context.Background(), root); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("docs = %d, want 1", got)
	}
	if err := w.ScanOnce(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing root must still fail the scan")
	}
}

func chunkMatches(c *Chunk, f Filter) bool {
	return newFilterMatcher(f).matches(c)
}
