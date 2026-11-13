package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		for _, tc := range []struct {
			field    string
			document Document
		}{
			{"id", Document{ID: bad, Text: "ok"}}, {"text", Document{ID: "a", Text: bad}},
			{"source", Document{ID: "a", Text: "ok", Source: bad}}, {"owner", Document{ID: "a", Text: "ok", Owner: bad}},
			{"metadata", Document{ID: "a", Text: "ok", Metadata: map[string]any{"nested": json.RawMessage(`"` + strings.ReplaceAll(bad, "\x00", `\u0000`) + `"`)}}},
		} {
			err := idx.Add(context.Background(), Document{ID: "valid", Text: "ok"}, tc.document)
			var refusal *InvalidDocumentTextError
			if !errors.As(err, &refusal) || refusal.Field != tc.field {
				t.Fatalf("Add(%+v): %v, want typed %s refusal", tc.document, err, tc.field)
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

func TestAddMetadataMatchesStoredJSON(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for _, tc := range []struct {
		name     string
		metadata map[string]any
		refused  bool
		encoding bool
	}{
		{"huge_number_and_nul", map[string]any{"raw": json.RawMessage(`{"number":1e400,"text":"a\u0000b"}`)}, true, false},
		{"huge_number", map[string]any{"raw": json.RawMessage(`{"number":1e400}`)}, false, false},
		{"omitted_field", map[string]any{"value": struct {
			Hidden  string `json:"-"`
			Visible string `json:"visible"`
		}{Hidden: "a\x00b", Visible: "ok"}}, false, false},
		{"encoding_failure", map[string]any{"value": make(chan int)}, true, true},
		{"nan_is_encoding_failure", map[string]any{"value": math.NaN()}, true, true},
		{"lone_high_surrogate", map[string]any{"raw": json.RawMessage(`"\ud800"`)}, true, false},
		{"lone_low_surrogate", map[string]any{"raw": json.RawMessage(`{"k":"a\udc00b"}`)}, true, false},
		{"high_then_text", map[string]any{"raw": json.RawMessage(`"\ud800x"`)}, true, false},
		{"high_then_high", map[string]any{"raw": json.RawMessage(`"\ud800\ud800"`)}, true, false},
		{"surrogate_key", map[string]any{"raw": json.RawMessage(`{"\udfff":1}`)}, true, false},
		{"surrogate_pair", map[string]any{"raw": json.RawMessage(`"\ud83d\ude00"`)}, false, false},
		{"escaped_backslash_u", map[string]any{"raw": json.RawMessage(`"\\ud800"`)}, false, false},
		{"normalized_utf8", map[string]any{"value": "a\xffb"}, false, false},
		{"raw_kind_utf8", map[string]any{"kind": "a\xffb"}, true, false},
		{"raw_kind_nul", map[string]any{"kind": "a\x00b"}, true, false},
		{"valid_kind", map[string]any{"kind": "guide"}, false, false},
		{"duplicate_key_nul_first", map[string]any{"raw": json.RawMessage(`{"x":"\u0000","x":"clean"}`)}, true, false},
		{"duplicate_key_nul_last", map[string]any{"raw": json.RawMessage(`{"x":"clean","x":"\u0000"}`)}, true, false},
		{"nested_duplicate_key_nul", map[string]any{"raw": json.RawMessage(`[{"x":"\u0000","x":"clean"}]`)}, true, false},
		{"duplicate_key_clean", map[string]any{"raw": json.RawMessage(`{"x":"first","x":"clean"}`)}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := idx.Add(context.Background(), Document{ID: tc.name, Text: "ok", Metadata: tc.metadata})
			var invalid *InvalidDocumentTextError
			if tc.encoding {
				if err == nil || errors.As(err, &invalid) || !strings.Contains(err.Error(), "metadata cannot be encoded as JSON") {
					t.Fatalf("Add: %v; want a JSON encoding refusal, not an invalid-text one", err)
				}
			} else if tc.refused {
				if !errors.As(err, &invalid) || invalid.Field != "metadata" {
					t.Fatalf("Add: %v; want metadata refusal", err)
				}
			} else if err != nil {
				t.Fatalf("stored JSON is valid: %v", err)
			}
		})
	}
}

// The bounds were measured on Postgres 16 jsonb: at most 131072 digits
// before the decimal point and 16383 after it, counted for zero too.
// The route decodes metadata numbers with UseNumber, so a literal jsonb
// cannot hold reaches Add; it is the caller's to fix, never a 500.
func TestIndexRouteRefusesOutOfRangeNumber(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	request := httptest.NewRequest(http.MethodPost, "/index", strings.NewReader(`{"documents":[{"id":"a","text":"ok","metadata":{"n":1e200000}}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	Handler(idx, WithInsecureDisabledAuth()).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"reason":"document_metadata_number_out_of_range"`) {
		t.Fatalf("status %d, body %s; want 400 document_metadata_number_out_of_range", response.Code, response.Body)
	}
	if stats := idx.Stats(); stats.Chunks != 0 {
		t.Fatalf("refused document reached storage: %+v", stats)
	}
}

func TestAddRefusesNumbersJSONBCannotHold(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for _, tc := range []struct {
		number  string
		refused bool
	}{
		{"1e400", false}, {"1e131071", false}, {"9.99e131071", false}, {"-1e131071", false},
		{"1e-16383", false}, {"1.5e-16382", false}, {"0e200000", false}, {"12.5", false},
		{"1e131072", true}, {"1e200000", true}, {"-1e131072", true}, {"0.5e131073", true},
		{"1e-16384", true}, {"1.5e-16383", true}, {"0.0000e-20000", true},
		{"1e99999999999999999999", true}, {"0e-99999999999999999999", true},
		{"0e1073741823", false}, {"0e1073741824", true},
	} {
		err := idx.Add(context.Background(), Document{ID: "n", Text: "ok", Metadata: map[string]any{"raw": json.RawMessage(`{"n":` + tc.number + `}`)}})
		var outOfRange *MetadataNumberRangeError
		if got := errors.As(err, &outOfRange); got != tc.refused || (!tc.refused && err != nil) {
			t.Errorf("Add(%s): %v; want refused=%v with no other error", tc.number, err, tc.refused)
		}
	}
}

func TestAddValidatesOnlyLastDocumentPerID(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Add(context.Background(), Document{ID: "same", Text: "superseded\x00"}, Document{ID: "same", Text: "kept"}); err != nil {
		t.Fatalf("superseded document refused: %v", err)
	}
	if got := idx.Stats().Docs; got != 1 {
		t.Fatalf("docs=%d, want 1", got)
	}
}

func TestOpenKeepsLegacyTextOutOfKeywordIndex(t *testing.T) {
	for _, mode := range []string{"snapshot", "wal"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx := context.Background()
			embedder := NewStubEmbedder(4)
			chunks := []Chunk{{ID: "bad:0", DocID: "bad", Text: "old\x00text", Vec: []float32{1, 0, 0, 0}}, {ID: "good:0", DocID: "good", Text: "clean", Vec: []float32{0, 1, 0, 0}}}
			if mode == "snapshot" {
				store := NewFlatStore(4, embedder.Name())
				if err := store.Add(ctx, chunks); err != nil {
					t.Fatal(err)
				}
				if err := store.Snapshot(filepath.Join(dir, "store.snap")); err != nil {
					t.Fatal(err)
				}
			} else {
				idx, err := Open(Options{Embedder: embedder, Path: dir})
				if err != nil {
					t.Fatal(err)
				}
				if err := idx.(*index).wal.append(walEntry{Op: walOpAdd, Chunks: chunks, Model: embedder.Name()}); err != nil {
					t.Fatal(err)
				}
				crash(idx)
			}
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })
			backend := &ownerRecorder{Backend: search.NewMemory()}
			idx, err := Open(Options{Embedder: embedder, Path: dir, Keyword: WrapSearchBackend(backend)})
			if err != nil {
				t.Fatalf("legacy %s cannot open: %v", mode, err)
			}
			t.Cleanup(func() { _ = idx.Close() })
			if got := idx.Stats().Docs; got != 2 {
				t.Fatalf("docs=%d, want both persisted docs", got)
			}
			if len(backend.docs) == 0 {
				t.Fatal("clean persisted chunk never indexed")
			}
			for _, doc := range backend.docs {
				if doc.ID == "bad:0" {
					t.Fatal("invalid legacy chunk reached keyword backend")
				}
			}
			if !strings.Contains(logs.String(), "skipped stored chunk") || !strings.Contains(logs.String(), "document text") {
				t.Fatalf("missing attributed warning: %s", logs.String())
			}
			if err := idx.Add(ctx, Document{ID: "new", Text: "live\x00text"}); err == nil {
				t.Fatal("live Add accepted invalid text")
			}
		})
	}
}

// MemoryKeyword holds any Go string, so a reopen keeps legacy chunks in
// its keyword leg: only a backend that refuses the text skips them.
func TestOpenKeepsLegacyTextInMemoryKeyword(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	embedder := NewStubEmbedder(4)
	store := NewFlatStore(4, embedder.Name())
	if err := store.Add(ctx, []Chunk{{ID: "bad:0", DocID: "bad", Text: "legacy\x00zebra", Vec: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Snapshot(filepath.Join(dir, "store.snap")); err != nil {
		t.Fatal(err)
	}
	keyword := NewMemoryKeyword()
	idx, err := Open(Options{Embedder: embedder, Path: dir, Keyword: keyword})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	hits, err := keyword.Search(ctx, "zebra", 5)
	if err != nil || len(hits) != 1 || hits[0].ChunkID != "bad:0" {
		t.Fatalf("keyword hits=%v, %v; want the legacy chunk kept", hits, err)
	}
}
