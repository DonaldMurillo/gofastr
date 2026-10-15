package semantic

import (
	"context"
	"errors"
	"github.com/DonaldMurillo/gofastr/battery/search"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestMMRFullDiversityPrefersDistinct(t *testing.T) {
	hits := []Hit{{Chunk: Chunk{ID: "a", Vec: []float32{1, 0}}}, {Chunk: Chunk{ID: "dup", Vec: []float32{1, 0}}}, {Chunk: Chunk{ID: "b", Vec: []float32{0, 1}}}}
	got := mmr([]float32{1, 0}, hits, 1, 2)
	if len(got) != 2 || got[1].Chunk.ID != "b" {
		t.Fatalf("%v", got)
	}
}
func TestMMRAccountsForNegativeSimilarity(t *testing.T) {
	hits := []Hit{{Chunk: Chunk{ID: "a", Vec: []float32{1, 0}}}, {Chunk: Chunk{ID: "orthogonal", Vec: []float32{0, 1}}}, {Chunk: Chunk{ID: "opposite", Vec: []float32{-1, 0}}}}
	got := mmr([]float32{1, 0}, hits, 0.9, 2)
	if len(got) != 2 || got[1].Chunk.ID != "opposite" {
		t.Fatalf("%v", got)
	}
}
func TestQueryRejectsInvalidMMRLambda(t *testing.T) {
	idx, e := Open(Options{Embedder: NewStubEmbedder(8)})
	if e != nil {
		t.Fatal(e)
	}
	defer idx.Close()
	for _, v := range []float64{math.NaN(), math.Inf(1), -0.1, 1.1} {
		if _, e := idx.Query(context.Background(), Query{Text: "q", MMRLambda: v}); e == nil {
			t.Errorf("accepted invalid lambda %v", v)
		}
	}
}

func TestKeywordDuplicateTermsDoNotChangeRank(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryKeyword()
	m.Index(ctx, "a", "alpha")
	m.Index(ctx, "b", "beta")
	one, _ := m.Search(ctx, "alpha beta", 2)
	two, _ := m.Search(ctx, "alpha beta beta", 2)
	if len(one) != len(two) {
		t.Fatal("different counts")
	}
	for i := range one {
		if one[i] != two[i] {
			t.Fatalf("duplicate query term altered score/rank: %v %v", one, two)
		}
	}
}

type scopeRoundTrip func(*http.Request) (*http.Response, error)

func (f scopeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func scopeOllama(body string, dim int) *OllamaEmbedder {
	return NewOllamaEmbedder(OllamaConfig{Dim: dim, Client: &http.Client{Transport: scopeRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}})
}
func TestOllamaRejectsInconsistentDimensions(t *testing.T) {
	for _, e := range []*OllamaEmbedder{scopeOllama(`{"embeddings":[[1],[1,0]]}`, 0), scopeOllama(`{"embeddings":[[1],[1]]}`, 2)} {
		if _, err := e.Embed(context.Background(), []string{"a", "b"}); err == nil {
			t.Fatal("accepted incompatible dimensions")
		}
	}
}
func TestOllamaRejectsEmptyEmbeddings(t *testing.T) {
	if _, e := scopeOllama(`{"embeddings":[[]]}`, 0).Embed(context.Background(), []string{"a"}); e == nil {
		t.Fatal("accepted empty embedding")
	}
}
func TestOllamaResponseCapDetectsExtraBytes(t *testing.T) {
	body := `{"embeddings":[[1]]}` + strings.Repeat(" ", 10<<20)
	if _, e := scopeOllama(body, 0).Embed(context.Background(), []string{"a"}); e == nil {
		t.Fatal("response cap silently discarded extra bytes")
	}
}
func TestKeywordTermCapHandlesUnicodeWhitespace(t *testing.T) {
	s := strings.Repeat("x\u2003", 80)
	got := boundedKeywordText(s)
	if len(strings.Fields(got)) > 64 {
		t.Fatal("unicode whitespace bypassed keyword cap")
	}
}

type scopePanicBackend struct{ search.Backend }

func (scopePanicBackend) Search(context.Context, search.Query) ([]search.Result, error) {
	panic("scope backend")
}
func TestSearchAdapterContainsSearchPanic(t *testing.T) {
	a := WrapSearchBackend(scopePanicBackend{})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic escaped: %v", r)
		}
	}()
	if _, e := a.Search(context.Background(), "q", 1); e == nil {
		t.Fatal("expected error")
	}
}
func TestMemoryKeywordHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewMemoryKeyword()
	if e := m.Index(ctx, "a", "x"); !errors.Is(e, context.Canceled) {
		t.Errorf("index=%v", e)
	}
	if e := m.Delete(ctx, "a"); !errors.Is(e, context.Canceled) {
		t.Errorf("delete=%v", e)
	}
	if _, e := m.Search(ctx, "x", 1); !errors.Is(e, context.Canceled) {
		t.Errorf("search=%v", e)
	}
}
