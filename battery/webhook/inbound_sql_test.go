package webhook

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func openInboundSQLStore(t *testing.T) (*sql.DB, *SQLInboundStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLInboundStore(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return db, s
}

func sampleEnvelope(id string) InboundEnvelope {
	return InboundEnvelope{
		ID:         id,
		Source:     "github",
		DedupeKey:  "del-" + id,
		Headers:    map[string]string{"X-Github-Event": "push"},
		Payload:    []byte(`{"event":"push"}`),
		Status:     InboundStatusReceived,
		Attempts:   0,
		LastError:  "",
		ReceivedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt:  time.Now().UTC().Truncate(time.Second),
	}
}

func TestSQLInbound_RoundTrip(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	env := sampleEnvelope("e-1")
	if err := s.AddEnvelope(ctx, env); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := s.GetEnvelope(ctx, "e-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Source != env.Source || got.Status != InboundStatusReceived ||
		string(got.Payload) != string(env.Payload) || got.DedupeKey != env.DedupeKey {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Headers["X-Github-Event"] != "push" {
		t.Errorf("header not restored: %v", got.Headers)
	}
	if !got.ReceivedAt.Equal(env.ReceivedAt) {
		t.Errorf("received_at drift: got %v want %v", got.ReceivedAt, env.ReceivedAt)
	}
}

func TestSQLInbound_GetMissing(t *testing.T) {
	_, s := openInboundSQLStore(t)
	got, err := s.GetEnvelope(context.Background(), "nope")
	if err != nil || got != nil {
		t.Errorf("missing id: got=%v err=%v, want (nil,nil)", got, err)
	}
}

func TestSQLInbound_UpdateLifecycle(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	_ = s.AddEnvelope(ctx, sampleEnvelope("e-2"))

	// Simulate the ProcessInbound transitions.
	up, _ := s.GetEnvelope(ctx, "e-2")
	up.Status = InboundStatusFailed
	up.Attempts = 3
	up.LastError = "boom"
	up.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := s.UpdateEnvelope(ctx, *up); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetEnvelope(ctx, "e-2")
	if got.Status != InboundStatusFailed || got.Attempts != 3 || got.LastError != "boom" {
		t.Errorf("update not applied: %+v", got)
	}
}

func TestSQLInbound_ClearedDedupeKeyPersists(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	_ = s.AddEnvelope(ctx, sampleEnvelope("e-9"))

	// The ingest handler clears the dedupe key when an enqueue failure
	// strands an envelope — the SQL store must persist that, or the
	// sender's redelivery stays dedupe-blocked forever.
	up, _ := s.GetEnvelope(ctx, "e-9")
	up.Status = InboundStatusFailed
	up.DedupeKey = ""
	if err := s.UpdateEnvelope(ctx, *up); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetEnvelope(ctx, "e-9")
	if got.DedupeKey != "" {
		t.Fatalf("dedupe key = %q after clearing, want empty", got.DedupeKey)
	}
	seen, err := s.SeenDedupeKey(ctx, "github", "del-e-9")
	if err != nil {
		t.Fatalf("seen: %v", err)
	}
	if seen {
		t.Fatal("cleared dedupe key still blocks redelivery")
	}
}

func TestSQLInbound_ListByStatusAndLimit(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	for i, st := range []string{InboundStatusReceived, InboundStatusProcessed, InboundStatusReceived} {
		e := sampleEnvelope("e-l-" + string(rune('a'+i)))
		e.Status = st
		e.ReceivedAt = time.Now().Add(time.Duration(i) * time.Second).UTC()
		_ = s.AddEnvelope(ctx, e)
	}
	got, err := s.ListEnvelopes(ctx, InboundStatusReceived, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("received count = %d, want 2", len(got))
	}
	// Newest-first ordering.
	if len(got) == 2 && !got[0].ReceivedAt.After(got[1].ReceivedAt) {
		t.Errorf("not newest-first")
	}
	// Limit cap.
	limited, _ := s.ListEnvelopes(ctx, "", 1)
	if len(limited) != 1 {
		t.Errorf("limit = %d, want 1", len(limited))
	}
}

func TestSQLInbound_SeenDedupeKey(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	e := sampleEnvelope("e-d")
	e.DedupeKey = "k-9"
	_ = s.AddEnvelope(ctx, e)

	seen, err := s.SeenDedupeKey(ctx, "github", "k-9")
	if err != nil || !seen {
		t.Errorf("seen github/k-9 = %v %v, want true", seen, err)
	}
	seen, err = s.SeenDedupeKey(ctx, "github", "other")
	if err != nil || seen {
		t.Errorf("seen github/other = %v %v, want false", seen, err)
	}
	seen, err = s.SeenDedupeKey(ctx, "stripe", "k-9")
	if err != nil || seen {
		t.Errorf("seen stripe/k-9 = %v %v, want false (source differs)", seen, err)
	}
	// Empty key short-circuits to (false, nil) without error.
	seen, err = s.SeenDedupeKey(ctx, "github", "")
	if err != nil || seen {
		t.Errorf("empty key = %v %v, want false", seen, err)
	}
}

func TestSQLInbound_EmptyDedupeKeyAllowed(t *testing.T) {
	// Two requests with empty dedupe keys must coexist (no unique constraint
	// would forbid them, and empty key means "no dedupe").
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	e1 := sampleEnvelope("e-x")
	e1.DedupeKey = ""
	e2 := sampleEnvelope("e-y")
	e2.DedupeKey = ""
	if err := s.AddEnvelope(ctx, e1); err != nil {
		t.Fatalf("add e1: %v", err)
	}
	if err := s.AddEnvelope(ctx, e2); err != nil {
		t.Fatalf("add e2: %v", err)
	}
	all, _ := s.ListEnvelopes(ctx, "", 0)
	if len(all) != 2 {
		t.Errorf("expected 2 empty-key envelopes, got %d", len(all))
	}
}

func TestSQLInbound_UnsafeTableRejected(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// SQL injection attempt via table name — safeIdent must reject it.
	if _, err := NewSQLInboundStore(db, WithInboundTable("t; DROP TABLE x")); err == nil {
		t.Errorf("expected error for unsafe table name")
	}
}

func TestSQLInbound_NilDBRejected(t *testing.T) {
	if _, err := NewSQLInboundStore(nil); err == nil {
		t.Errorf("expected error for nil DB")
	}
}

func TestSQLInbound_CustomTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	s, err := NewSQLInboundStore(db, WithInboundTable("custom_inbound"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if s.table != "custom_inbound" {
		t.Errorf("table = %q", s.table)
	}
	// Confirm the custom table was created and is usable.
	if err := s.AddEnvelope(context.Background(), sampleEnvelope("e-c")); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := s.GetEnvelope(context.Background(), "e-c")
	if got == nil {
		t.Fatalf("envelope not found in custom table")
	}
}

// TestSQLInbound_HeadersJSON exercises nil/empty/single header encodings
// to confirm the JSON column round-trips all of them.
func TestSQLInbound_HeadersJSON(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()

	cases := []struct {
		name string
		h    map[string]string
	}{
		{"nil", nil},
		{"empty", map[string]string{}},
		{"single", map[string]string{"X-Github-Event": "ping"}},
		{"multi", map[string]string{"X-Github-Event": "push", "X-Foo": "bar"}},
	}
	for i, c := range cases {
		e := sampleEnvelope("hdr-" + string(rune('a'+i)))
		e.Headers = c.h
		if err := s.AddEnvelope(ctx, e); err != nil {
			t.Fatalf("%s add: %v", c.name, err)
		}
		got, _ := s.GetEnvelope(ctx, e.ID)
		if c.h == nil || len(c.h) == 0 {
			if got.Headers != nil {
				t.Errorf("%s: expected nil headers, got %v", c.name, got.Headers)
			}
			continue
		}
		if len(got.Headers) != len(c.h) {
			t.Errorf("%s: header count = %d, want %d (%v)", c.name, len(got.Headers), len(c.h), got.Headers)
		}
	}
}

// TestSQLInbound_IngestHandlerIntegration wires the SQL store behind the
// real handler to confirm the store satisfies the handler's calls.
func TestSQLInbound_IngestHandlerIntegration(t *testing.T) {
	_, store := openInboundSQLStore(t)
	const secret = "s3cr3t"
	body := []byte(`{"sql":true}`)
	h, err := IngestHandler(IngestConfig{
		Source:        "github",
		Verifier:      TimestampedVerifier(secret, 5*time.Minute),
		Store:         store,
		JobType:       "webhook.inbound",
		KeepHeaders:   []string{"X-Github-Event"},
		DedupeKeyFunc: func(r *http.Request, _ []byte) string { return "sql-delivery" },
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		SignatureHeader:  SignWithTimestamp(secret, time.Now().Unix(), body),
		"X-GitHub-Event": "push",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	seen, _ := store.SeenDedupeKey(context.Background(), "github", "sql-delivery")
	if !seen {
		t.Error("dedupe key not persisted by SQL store")
	}
}
