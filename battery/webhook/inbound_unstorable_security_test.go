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

	t.Run("dedupe key NUL", func(t *testing.T) {
		store := NewMemoryInboundStore()
		h, err := IngestHandler(IngestConfig{
			Source:        "github",
			Verifier:      TimestampedVerifier(secret, 5*time.Minute),
			Store:         store,
			DedupeKeyFunc: func(r *http.Request, _ []byte) string { return "d\x00k" },
		})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, newIngestRequest(http.MethodPost, string(body), map[string]string{SignatureHeader: sign}))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		if envs, err := store.ListEnvelopes(context.Background(), "", 10); err != nil || len(envs) != 0 {
			t.Fatalf("unstorable key persisted: envs=%d err=%v", len(envs), err)
		}
	})
	t.Run("owner invalid UTF-8", func(t *testing.T) {
		store := NewMemoryInboundStore()
		h, err := IngestHandler(IngestConfig{
			Source:   "github",
			Verifier: TimestampedVerifier(secret, 5*time.Minute),
			Store:    store,
			Owner:    func(r *http.Request, _ []byte) string { return "o\xffp" },
		})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, newIngestRequest(http.MethodPost, string(body), map[string]string{SignatureHeader: sign}))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		if envs, err := store.ListEnvelopes(context.Background(), "", 10); err != nil || len(envs) != 0 {
			t.Fatalf("unstorable owner persisted: envs=%d err=%v", len(envs), err)
		}
	})
	t.Run("storable key and owner persist", func(t *testing.T) {
		store := NewMemoryInboundStore()
		h, err := IngestHandler(IngestConfig{
			Source:        "github",
			Verifier:      TimestampedVerifier(secret, 5*time.Minute),
			Store:         store,
			DedupeKeyFunc: func(r *http.Request, _ []byte) string { return "abc-123" },
			Owner:         func(r *http.Request, _ []byte) string { return "u-1" },
		})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, newIngestRequest(http.MethodPost, string(body), map[string]string{SignatureHeader: sign}))
		if rr.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", rr.Code)
		}
	})
}
