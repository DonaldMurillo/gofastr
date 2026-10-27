package semantic

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type refusingChunker struct{}

func (refusingChunker) Chunk(Document) ([]Chunk, error) {
	return nil, errors.New("chunker internals: offset 17 in shard 4")
}

// A document the chunker refuses is the caller's fault, and so is one
// with no id: /index answered both 500 "internal error". Each is a 400
// naming the refusal, with the chunker's own detail kept off the wire.
func TestIndexRouteRefusesBadDocument(t *testing.T) {
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Chunker: refusingChunker{}})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	h := Handler(idx, WithInsecureDisabledAuth())
	for body, reason := range map[string]string{
		`{"documents":[{"id":"x","text":"anything"}]}`: "document_unchunkable",
		`{"documents":[{"id":"","text":"anything"}]}`:  "doc_id_is_required",
	} {
		req := httptest.NewRequest(http.MethodPost, "/index", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400: %s", body, rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `"reason":"`+reason+`"`) {
			t.Errorf("%s: want reason %s: %s", body, reason, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "shard") {
			t.Errorf("%s: the chunker's detail reached the wire: %s", body, rec.Body)
		}
	}
}
