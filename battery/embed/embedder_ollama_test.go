package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOllamaServer returns an httptest.Server that mimics Ollama's
// /api/embed endpoint. Embeddings are deterministic FNV-hash bags so
// repeated calls with the same input return the same vector.
func fakeOllamaServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req ollamaRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Reuse the stub embedder for deterministic vectors. The
		// server returns un-normalised values so the client's L2
		// normalisation step is exercised.
		emb := NewStubEmbedder(dim)
		vecs, _ := emb.Embed(r.Context(), req.Input)
		// Multiply by 10 to break the unit-length property; the client
		// is responsible for re-normalising.
		for i := range vecs {
			for j := range vecs[i] {
				vecs[i][j] *= 10
			}
		}
		json.NewEncoder(w).Encode(ollamaResponse{Embeddings: vecs})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOllamaEmbedderBatchAndNormalise(t *testing.T) {
	srv := fakeOllamaServer(t, 64)
	e := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"})

	vecs, err := e.Embed(context.Background(), []string{"alpha", "bravo", "charlie"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d vecs, want 3", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 64 {
			t.Fatalf("vec %d dim = %d, want 64", i, len(v))
		}
		var sumsq float64
		for _, x := range v {
			sumsq += float64(x) * float64(x)
		}
		if sumsq < 0.99 || sumsq > 1.01 {
			t.Fatalf("vec %d not L2-normalised: sumsq=%f", i, sumsq)
		}
	}
	if e.Dim() != 64 {
		t.Fatalf("Dim after Embed = %d, want 64 (auto-detected)", e.Dim())
	}
	if e.Name() != "ollama:test-model" {
		t.Fatalf("Name = %q, want ollama:test-model", e.Name())
	}
}

func TestOllamaEmbedderPropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model 'missing' not found, try pulling it first"}`))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "missing"})
	_, err := e.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "pulling it first") {
		t.Fatalf("error should surface the server message verbatim: %v", err)
	}
}

func TestOllamaEmbedderUnreachableServer(t *testing.T) {
	// Use a port we know nothing is listening on. http.Client with a
	// short timeout makes this fast.
	e := NewOllamaEmbedder(OllamaConfig{
		BaseURL: "http://127.0.0.1:1", // port 1 is reserved
	})
	_, err := e.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("expected connection error, got nil")
	}
	// The error should hint at the configuration issue.
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("error should mention `ollama serve`: %v", err)
	}
}

func TestOllamaEmbedderIntegratesWithIndex(t *testing.T) {
	srv := fakeOllamaServer(t, 64)
	idx, err := Open(Options{
		Embedder: NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"}),
		Keyword:  NewMemoryKeyword(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	idx.Add(ctx,
		Document{ID: "a", Text: "the cache battery provides redis and memory backends"},
		Document{ID: "b", Text: "the auth battery provides sessions and JWTs"},
	)
	hits, err := idx.Query(ctx, Query{Text: "cache battery", K: 2, Hybrid: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) == 0 || hits[0].Chunk.DocID != "a" {
		t.Fatalf("top hit = %+v, want doc=a", hits)
	}
}

func TestOllamaEmbedderProbe(t *testing.T) {
	srv := fakeOllamaServer(t, 32)
	e := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL})
	if err := e.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if e.Dim() != 32 {
		t.Fatalf("Dim after Probe = %d, want 32", e.Dim())
	}
}

func TestOllamaEmbedderFallsBackToOldSingleEmbeddingShape(t *testing.T) {
	// Older Ollama: returns {"embedding":[...]} instead of {"embeddings":[[...]]}.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ollamaRequest
		_ = json.Unmarshal(body, &req)
		emb := NewStubEmbedder(16)
		vecs, _ := emb.Embed(r.Context(), req.Input)
		json.NewEncoder(w).Encode(ollamaResponse{Embedding: vecs[0]})
	}))
	defer srv.Close()
	e := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL})
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 16 {
		t.Fatalf("vecs = %+v", vecs)
	}
}

// TestOllamaEmbedder_DimRaceFree exercises the lazy dim cache under concurrent
// Embed (writer) and Dim (reader) with Dim left unset — the exact shape the
// previous int field raced on. Run with -race; it must be clean.
func TestOllamaEmbedder_DimRaceFree(t *testing.T) {
	srv := fakeOllamaServer(t, 32)
	e := NewOllamaEmbedder(OllamaConfig{BaseURL: srv.URL, Model: "test-model"}) // Dim unset

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = e.Dim()
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		if _, err := e.Embed(context.Background(), []string{"x"}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
	}
	<-done
	if e.Dim() != 32 {
		t.Fatalf("Dim = %d, want 32", e.Dim())
	}
}
