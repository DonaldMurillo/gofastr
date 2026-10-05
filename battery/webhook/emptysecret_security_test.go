package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An empty secret keys the HMAC with "", so anyone who knows the body can
// compute the signature. HMACSHA256Verifier must fail closed on it, the way
// VerifyTimestamped does (TestVerifyTimestamped_RejectsEmptySecret), and the
// forged payload must never be persisted.
func TestHMACVerifierEmptySecretRefuses(t *testing.T) {
	store := NewMemoryInboundStore()
	h, err := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: HMACSHA256Verifier("X-Hub-Signature-256", "sha256=", ""),
		Store:    store,
	})
	if err != nil {
		t.Fatalf("IngestHandler: %v", err)
	}
	body := []byte(`{"action":"forged-by-anyone"}`)
	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		"X-Hub-Signature-256": ghSignature(body, ""),
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty-key signature: status %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	envs, err := store.ListEnvelopes(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 0 {
		t.Fatalf("forged envelope persisted: %d rows", len(envs))
	}
}
