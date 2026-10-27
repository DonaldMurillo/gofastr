package semantic

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// eachHTTPStore runs body against a handler over FlatStore, a persisted
// FlatStore (the WAL must encode what the handler decodes) and
// PgVectorStore.
func eachHTTPStore(t *testing.T, dim int, body func(t *testing.T, srv *httptest.Server)) {
	t.Helper()
	emb := NewStubEmbedder(dim)
	variants := []struct {
		name string
		opts func(t *testing.T) Options
	}{
		{"flat", func(*testing.T) Options { return Options{Embedder: emb} }},
		{"flat-persisted", func(t *testing.T) Options { return Options{Embedder: emb, Path: t.TempDir()} }},
		{"pgvector", func(t *testing.T) Options {
			s, err := NewPgVector(openPgVector(t), PgVectorConfig{Dim: dim, Model: emb.Name()})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureSchema(t.Context()); err != nil {
				t.Fatal(err)
			}
			return Options{Embedder: emb, Store: s}
		}},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			idx, err := Open(v.opts(t))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = idx.Close() })
			srv := httptest.NewServer(Handler(idx, WithAuthToken("test")))
			t.Cleanup(srv.Close)
			body(t, srv)
		})
	}
}

// A document whose text has no tokens embeds as the zero vector. Its
// cosine distance is NaN on pgvector, and a NaN score made the handler
// commit 200 and then fail to encode, leaving an empty body. The score
// is finite (zero, as FlatStore scores it) and the body decodes.
func TestZeroVectorScoresFinite(t *testing.T) {
	eachHTTPStore(t, 2, func(t *testing.T, srv *httptest.Server) {
		resp := postJSONAuthed(t, srv.URL+"/index", `{"documents":[{"id":"d","text":"!!!"}]}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("index = %d", resp.StatusCode)
		}
		resp = postJSONAuthed(t, srv.URL+"/query", `{"text":"needle","k":1}`)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("query = %d %s", resp.StatusCode, raw)
		}
		var out queryResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("query body %q does not decode: %v", raw, err)
		}
		if len(out.Hits) != 1 || out.Hits[0].Score != 0 {
			t.Fatalf("hits = %+v, want doc d with score 0", out.Hits)
		}
	})
}

// A value that cannot encode answers 500 with an error body, never 200
// with an empty one: the status is sent only after the body encodes.
func TestWriteJSONEncodeFailureIs500(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	writeJSON(rec, req, http.StatusOK, map[string]float64{"score": math.NaN()})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var e errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Reason == "" {
		t.Fatalf("body %q is not an error response (%v)", rec.Body.String(), err)
	}
}

// Two tenants whose snowflake-sized IDs differ only past float64's 53-bit
// mantissa. A filter on one must not return the other's document.
func TestMetaMatchKeepsLargeIntegers(t *testing.T) {
	eachHTTPStore(t, 16, func(t *testing.T, srv *httptest.Server) {
		resp := postJSONAuthed(t, srv.URL+"/index", `{"documents":[
			{"id":"a","text":"needle","metadata":{"tenant":9007199254740993}},
			{"id":"b","text":"needle","metadata":{"tenant":9007199254740992}}]}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("index = %d", resp.StatusCode)
		}
		resp = postJSONAuthed(t, srv.URL+"/query",
			`{"text":"needle","k":10,"filter":{"meta_match":{"tenant":9007199254740993}}}`)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("query = %d %s", resp.StatusCode, raw)
		}
		var out queryResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		var docs []string
		for _, h := range out.Hits {
			docs = append(docs, h.Chunk.DocID)
		}
		if len(docs) != 1 || docs[0] != "a" {
			t.Fatalf("hits for tenant ...993 = %v, want [a]: a large integer was rounded to the other tenant's id", docs)
		}
	})
}
