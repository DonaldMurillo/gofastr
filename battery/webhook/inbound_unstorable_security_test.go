package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A DedupeKeyFunc or Owner callback that reads request bytes the SQL
// store cannot bind (a NUL byte, invalid UTF-8 — Postgres 22021) is
// refused with a 400 before anything persists: such a key could never
// dedupe and such an owner could never be attributed, and the first
// store write would have answered 500 where SQLite silently stored it.
func TestIngestUnstorableDedupeKeyAndOwnerRefused(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"event":"push"}`)
	sign := SignWithTimestamp(secret, time.Now().Unix(), "", "", body)

	for _, tc := range []struct {
		name, key, owner string
		code             int
	}{
		{"key NUL", "d\x00k", "owner", http.StatusBadRequest},
		{"key invalid UTF-8", "d\xffk", "owner", http.StatusBadRequest},
		{"owner NUL", "key", "o\x00p", http.StatusBadRequest},
		{"owner invalid UTF-8", "key", "o\xffp", http.StatusBadRequest},
		{"storable callbacks", "abc-123", "u-1", http.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryInboundStore()
			h, err := IngestHandler(IngestConfig{
				Source: "github", Verifier: TimestampedVerifier(secret, 5*time.Minute), Store: store,
				DedupeKeyFunc: func(*http.Request, []byte) string { return tc.key },
				Owner:         func(*http.Request, []byte) string { return tc.owner },
			})
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, newIngestRequest(http.MethodPost, string(body), map[string]string{SignatureHeader: sign}))
			if rr.Code != tc.code {
				t.Fatalf("status = %d, want %d", rr.Code, tc.code)
			}
			envs, err := store.ListEnvelopes(context.Background(), "", 10)
			want := 0
			if tc.code == http.StatusAccepted {
				want = 1
			}
			if err != nil || len(envs) != want {
				t.Fatalf("persisted %d envelopes, error %v; want %d", len(envs), err, want)
			}
		})
	}
}
