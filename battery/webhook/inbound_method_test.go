package webhook

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

func ingestHandlerForMethodTest(t *testing.T) http.Handler {
	t.Helper()
	h, err := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: HMACSHA256Verifier("X-Hub-Signature-256", "sha256=", "secret"),
		Store:    NewMemoryInboundStore(),
	})
	if err != nil {
		t.Fatalf("IngestHandler: %v", err)
	}
	return h
}

// A 405 from the ingest handler names POST in Allow, as RFC 9110
// requires; a host that mounts the handler on a bare path reaches this
// branch before any router-level refusal.
func TestIngestRefusesNonPostWithAllow(t *testing.T) {
	h := ingestHandlerForMethodTest(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hook", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want %q", got, http.MethodPost)
	}
}

// A body whose read fails is refused as unreadable, not as malformed
// JSON: the sender's payload never reached the verifier.
func TestIngestBodyReadFailureNamesTheUnreadableBody(t *testing.T) {
	h := ingestHandlerForMethodTest(t)
	req := httptest.NewRequest(http.MethodPost, "/hook", iotest.ErrReader(errors.New("connection reset")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "failed to read request body" {
		t.Errorf("body %q, want the body_unreadable phrase", got)
	}
}
