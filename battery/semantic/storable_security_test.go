package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/search"
)

func TestIndexRouteRefusesUnstorableText(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	handler := Handler(idx, WithInsecureDisabledAuth())
	for name, document := range map[string]string{
		"id":           `{"id":"a\u0000b","text":"ok"}`,
		"text":         `{"id":"a","text":"a\u0000b"}`,
		"source":       `{"id":"a","text":"ok","source":"a\u0000b"}`,
		"owner":        `{"id":"a","text":"ok","owner":"a\u0000b"}`,
		"metadata":     `{"id":"a","text":"ok","metadata":{"tag":"a\u0000b"}}`,
		"kind":         `{"id":"a","text":"ok","metadata":{"kind":"a\u0000b"}}`,
		"nested map":   `{"id":"a","text":"ok","metadata":{"nested":{"tag":"a\u0000b"}}}`,
		"nested slice": `{"id":"a","text":"ok","metadata":{"nested":[{"tag":"a\u0000b"}]}}`,
		"metadata key": `{"id":"a","text":"ok","metadata":{"a\u0000b":"ok"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/index", strings.NewReader(`{"documents":[`+document+`]}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"reason":"document_invalid_text"`) {
				t.Fatalf("status %d, body %s; want 400 with document_invalid_text", response.Code, response.Body)
			}
			if stats := idx.Stats(); stats.Chunks != 0 {
				t.Fatalf("refused document reached storage: %+v", stats)
			}
		})
	}
	if err := idx.Add(context.Background(), Document{ID: "valid", Text: "café\n東京", Metadata: map[string]any{"tags": []any{"go", 42, nil}}}); err != nil {
		t.Fatalf("valid Unicode document: %v", err)
	}
}

func TestAddRefusesUnstorableText(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for _, bad := range []string{"a\x00b", "a\xffb", "\xed\xa0\x80"} {
		for _, document := range []Document{
			{ID: bad, Text: "ok"}, {ID: "a", Text: bad},
			{ID: "a", Text: "ok", Source: bad}, {ID: "a", Text: "ok", Owner: bad},
			{ID: "a", Text: "ok", Metadata: map[string]any{"nested": []map[string][]string{{"tag": {bad}}}}},
			{ID: "a", Text: "ok", Metadata: map[string]any{bad: "ok"}},
		} {
			err := idx.Add(context.Background(), Document{ID: "valid", Text: "ok"}, document)
			var refusal *InvalidDocumentTextError
			if !errors.As(err, &refusal) || refusal.Field == "" {
				t.Fatalf("Add(%+v): %v, want typed text refusal", document, err)
			}
			if stats := idx.Stats(); stats.Chunks != 0 {
				t.Fatalf("refused batch partly stored: %+v", stats)
			}
		}
	}
}

func TestIndexOwnedRefusesUnstorableText(t *testing.T) {
	backend := &ownerRecorder{Backend: search.NewMemory()}
	adapter := WrapSearchBackend(backend).(OwnedKeywordBackend)
	for _, bad := range []string{"a\x00b", "a\xffb", "\xed\xa0\x80"} {
		for _, values := range [][3]string{{bad, "owner", "ok"}, {"a", bad, "ok"}, {"a", "owner", bad}} {
			err := adapter.IndexOwned(context.Background(), values[0], values[1], values[2])
			var refusal *InvalidDocumentTextError
			if !errors.As(err, &refusal) || len(backend.docs) != 0 {
				t.Fatalf("IndexOwned(%q): %v, stored %d documents", values, err, len(backend.docs))
			}
		}
	}
}

// Metadata is map[string]any, so a value can carry its own JSON: the
// stored JSONB holds what it marshals to, and Postgres refuses an escaped
// NUL there (22P05) as it refuses a raw one in text.
func TestAddRefusesMarshaledMetadataNUL(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for _, raw := range []string{`{"tag":"a\u0000b"}`, `{"a\u0000b":"ok"}`, `["a\u0000b"]`, "\"a\xffb\""} {
		err := idx.Add(context.Background(), Document{ID: "a", Text: "ok", Metadata: map[string]any{"nested": json.RawMessage(raw)}})
		var refusal *InvalidDocumentTextError
		if !errors.As(err, &refusal) || refusal.Field != "metadata" {
			t.Fatalf("Add(RawMessage %q): %v, want metadata refusal", raw, err)
		}
	}
	// An escaped backslash before u0000 is those six characters, not a NUL.
	if err := idx.Add(context.Background(), Document{ID: "a", Text: "ok", Metadata: map[string]any{"nested": json.RawMessage(`{"tag":"a\\u0000b"}`)}}); err != nil {
		t.Fatalf("escaped backslash: %v", err)
	}
}
