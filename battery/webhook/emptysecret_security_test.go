package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// blankSecrets key an HMAC anyone can compute: the empty string, and the
// whitespace a dotenv or YAML quoting slip leaves behind.
var blankSecrets = []string{"", " ", "\t", " \n "}

// An empty secret keys the HMAC with "", so anyone who knows the body can
// compute the signature. HMACSHA256Verifier must fail closed on it, the way
// VerifyTimestamped does, and the forged payload must never be persisted. A
// whitespace-only secret is the same guessable constant.
func TestHMACVerifierEmptySecretRefuses(t *testing.T) {
	for _, secret := range blankSecrets {
		store := NewMemoryInboundStore()
		h, err := IngestHandler(IngestConfig{
			Source:   "github",
			Verifier: HMACSHA256Verifier("X-Hub-Signature-256", "sha256=", secret),
			Store:    store,
		})
		if err != nil {
			t.Fatalf("IngestHandler: %v", err)
		}
		body := []byte(`{"action":"forged-by-anyone"}`)
		req := newIngestRequest(http.MethodPost, string(body), map[string]string{
			"X-Hub-Signature-256": ghSignature(body, secret),
		})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("secret %q: status %d, want 401 (body %q)", secret, rec.Code, rec.Body.String())
		}
		envs, err := store.ListEnvelopes(context.Background(), "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(envs) != 0 {
			t.Fatalf("secret %q: forged envelope persisted: %d rows", secret, len(envs))
		}
	}
}

func TestVerifyTimestampedBlankSecretRefuses(t *testing.T) {
	body := []byte("x")
	for _, secret := range blankSecrets {
		sig := SignWithTimestamp(secret, time.Now().Unix(), body)
		if VerifyTimestamped(secret, sig, body, time.Hour) {
			t.Errorf("secret %q verified a signature anyone can compute", secret)
		}
	}
}
