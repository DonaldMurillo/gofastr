package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// ----- test doubles ---------------------------------------------------------

// stubQueue records Enqueue calls and optionally returns err on Enqueue.
type stubQueue struct {
	mu   sync.Mutex
	jobs []queue.Job
	err  error
}

func (q *stubQueue) Enqueue(_ context.Context, job queue.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, job)
	return nil
}
func (q *stubQueue) Dequeue(context.Context, ...string) (queue.Job, error) {
	return queue.Job{}, queue.ErrNoJob
}
func (q *stubQueue) Ack(context.Context, string) error  { return nil }
func (q *stubQueue) Nack(context.Context, string) error { return nil }
func (q *stubQueue) Close() error                       { return nil }

func (q *stubQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// failingStore delegates everything to MemoryInboundStore but fails AddEnvelope.
type failingStore struct{ *MemoryInboundStore }

func (failingStore) AddEnvelope(context.Context, InboundEnvelope) error {
	return errors.New("store down")
}

// ----- helpers --------------------------------------------------------------

func newIngestRequest(method, body string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, "/hook", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func signedHeaders(body []byte, secret string) map[string]string {
	return map[string]string{
		SignatureHeader: SignWithTimestamp(secret, time.Now().Unix(), body),
	}
}

// ghSignature mimics GitHub's X-Hub-Signature-256 ("sha256=" + hex hmac).
func ghSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func respID(t *testing.T, body []byte) string {
	t.Helper()
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode response body %q: %v", body, err)
	}
	if m.ID == "" {
		t.Fatalf("response body %q has empty id", body)
	}
	return m.ID
}

func jobEnvelopeID(t *testing.T, j queue.Job) string {
	t.Helper()
	var p struct {
		EnvelopeID string `json:"envelope_id"`
		Source     string `json:"source"`
	}
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	return p.EnvelopeID
}

// ----- handler tests --------------------------------------------------------

func TestIngest_HappyPath(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"event":"push"}`)
	store := NewMemoryInboundStore()
	q := &stubQueue{}
	h, err := IngestHandler(IngestConfig{
		Source:      "github",
		Verifier:    TimestampedVerifier(secret, 5*time.Minute),
		Store:       store,
		Queue:       q,
		JobType:     "webhook.inbound",
		KeepHeaders: []string{"X-GitHub-Event", "X-GitHub-Delivery"},
		DedupeKeyFunc: func(r *http.Request, _ []byte) string {
			return r.Header.Get("X-GitHub-Delivery")
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		SignatureHeader:         SignWithTimestamp(secret, time.Now().Unix(), body),
		"X-GitHub-Event":        "push",
		"X-GitHub-Delivery":     "abc-123",
		"X-Secret-Leak-Attempt": "should-not-persist",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	id := respID(t, rr.Body.Bytes())

	env, err := store.GetEnvelope(context.Background(), id)
	if err != nil || env == nil {
		t.Fatalf("envelope not persisted: %v %v", env, err)
	}
	if env.Status != InboundStatusReceived {
		t.Errorf("status = %q, want received", env.Status)
	}
	if string(env.Payload) != string(body) {
		t.Errorf("payload mismatch")
	}
	// Allowlist: only X-Github-Event + X-Github-Delivery persisted, in
	// canonical (textproto) key form.
	if len(env.Headers) != 2 {
		t.Errorf("headers = %v, want exactly 2", env.Headers)
	}
	if env.Headers["X-Github-Event"] != "push" {
		t.Errorf("X-Github-Event missing/wrong from %v", env.Headers)
	}
	if env.Headers["X-Github-Delivery"] != "abc-123" {
		t.Errorf("X-Github-Delivery missing/wrong from %v", env.Headers)
	}
	if env.DedupeKey != "abc-123" {
		t.Errorf("dedupe key = %q, want abc-123", env.DedupeKey)
	}

	if q.count() != 1 {
		t.Fatalf("jobs enqueued = %d, want 1", q.count())
	}
	if got := jobEnvelopeID(t, q.jobs[0]); got != id {
		t.Errorf("job envelope_id = %q, want %q", got, id)
	}
}

func TestIngest_BadSignature_401_NoPersist(t *testing.T) {
	store := NewMemoryInboundStore()
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier("secret", 5*time.Minute),
		Store:    store,
		JobType:  "webhook.inbound",
		Queue:    &stubQueue{},
	})

	req := newIngestRequest(http.MethodPost, `{"x":1}`, map[string]string{
		SignatureHeader: "t=1,v1=deadbeef",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "signature verification failed") {
		t.Errorf("body should be generic, got %q", rr.Body.String())
	}
	all, _ := store.ListEnvelopes(context.Background(), "", 0)
	if len(all) != 0 {
		t.Errorf("unverified payload was persisted: %d envelopes", len(all))
	}
}

func TestIngest_OversizeBody_413(t *testing.T) {
	store := NewMemoryInboundStore()
	h, _ := IngestHandler(IngestConfig{
		Source:       "github",
		Verifier:     TimestampedVerifier("secret", 5*time.Minute),
		Store:        store,
		MaxBodyBytes: 8,
	})
	big := strings.Repeat("x", 1024)
	req := newIngestRequest(http.MethodPost, big, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
}

func TestIngest_NonPost_405(t *testing.T) {
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier("secret", 5*time.Minute),
		Store:    NewMemoryInboundStore(),
	})
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := newIngestRequest(m, "", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rr.Code)
		}
	}
}

func TestIngest_Dedupe_SecondGets200(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"e":1}`)
	store := NewMemoryInboundStore()
	q := &stubQueue{}
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		Queue:    q,
		JobType:  "webhook.inbound",
		DedupeKeyFunc: func(_ *http.Request, _ []byte) string {
			return "delivery-7"
		},
	})

	mk := func() *http.Request {
		return newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	}

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, mk())
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first: status = %d, want 202", rr1.Code)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, mk())
	if rr2.Code != http.StatusOK {
		t.Fatalf("second (dup): status = %d, want 200", rr2.Code)
	}

	all, _ := store.ListEnvelopes(context.Background(), "", 0)
	if len(all) != 1 {
		t.Errorf("envelopes = %d, want 1", len(all))
	}
	if q.count() != 1 {
		t.Errorf("jobs = %d, want 1", q.count())
	}
}

// Queueless ingestion still dedupes: with no Queue, the dedupe key is stored
// on the envelope up front (persistence IS durable acceptance), so a
// redelivery of the same key gets the idempotent 200 ack.
func TestIngest_QueueNil_Dedupes(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"e":1}`)
	store := NewMemoryInboundStore()
	h, _ := IngestHandler(IngestConfig{
		Source:   "stripe",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		// Queue intentionally nil.
		DedupeKeyFunc: func(_ *http.Request, _ []byte) string { return "delivery-9" },
	})
	mk := func() *http.Request {
		return newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	}
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, mk())
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first: status = %d, want 202", rr1.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, mk())
	if rr2.Code != http.StatusOK {
		t.Fatalf("second (dup): status = %d, want 200 (queueless dedupe broken)", rr2.Code)
	}
	all, _ := store.ListEnvelopes(context.Background(), "", 0)
	if len(all) != 1 {
		t.Errorf("envelopes = %d, want 1", len(all))
	}
}

func TestIngest_KeepHeadersAllowlist(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	store := NewMemoryInboundStore()
	h, _ := IngestHandler(IngestConfig{
		Source:      "github",
		Verifier:    TimestampedVerifier(secret, 5*time.Minute),
		Store:       store,
		KeepHeaders: []string{"X-GitHub-Event"},
	})
	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		SignatureHeader:   SignWithTimestamp(secret, time.Now().Unix(), body),
		"X-GitHub-Event":  "push",
		"X-Random-Header": "nope",
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rr.Code)
	}
	id := respID(t, rr.Body.Bytes())
	env, _ := store.GetEnvelope(context.Background(), id)
	if _, ok := env.Headers["X-Random-Header"]; ok {
		t.Errorf("non-allowlisted header persisted: %v", env.Headers)
	}
	if env.Headers["X-Github-Event"] != "push" {
		t.Errorf("allowlisted header missing: %v", env.Headers)
	}
}

func TestIngest_QueueNil_Persists_202(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	store := NewMemoryInboundStore()
	h, _ := IngestHandler(IngestConfig{
		Source:   "stripe",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		// Queue intentionally nil.
	})
	req := newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	all, _ := store.ListEnvelopes(context.Background(), "", 0)
	if len(all) != 1 {
		t.Errorf("envelopes = %d, want 1", len(all))
	}
}

func TestIngest_StoreFailure_500(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	h, _ := IngestHandler(IngestConfig{
		Source:   "stripe",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    failingStore{NewMemoryInboundStore()},
	})
	req := newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

func TestIngest_EnqueueFailure_500_NoOrphan(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	store := NewMemoryInboundStore()
	q := &stubQueue{err: errors.New("queue down")}
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		Queue:    q,
		JobType:  "webhook.inbound",
	})
	req := newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	// Exactly one envelope, and it must NOT be left as "received".
	all, _ := store.ListEnvelopes(context.Background(), "", 0)
	if len(all) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(all))
	}
	if all[0].Status == InboundStatusReceived {
		t.Errorf("orphan received envelope left after enqueue failure")
	}
	if all[0].Status != InboundStatusFailed {
		t.Errorf("status = %q, want failed", all[0].Status)
	}
	if all[0].LastError == "" {
		t.Errorf("LastError should record the enqueue failure")
	}
}

func TestIngest_RedeliveryAfterEnqueueFailure(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	store := NewMemoryInboundStore()
	q := &stubQueue{err: errors.New("queue down")}
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		Queue:    q,
		JobType:  "webhook.inbound",
		DedupeKeyFunc: func(r *http.Request, _ []byte) string {
			return r.Header.Get("X-GitHub-Delivery")
		},
	})

	send := func() *httptest.ResponseRecorder {
		headers := signedHeaders(body, secret)
		headers["X-GitHub-Delivery"] = "abc-123"
		req := newIngestRequest(http.MethodPost, string(body), headers)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	// First delivery: enqueue fails → 500, envelope marked failed.
	if rr := send(); rr.Code != http.StatusInternalServerError {
		t.Fatalf("first delivery status = %d, want 500", rr.Code)
	}

	// The sender retries the 500 with the SAME delivery id, queue healthy
	// again. The failed-at-ingest envelope must not dedupe-block the
	// retry, or the event is lost forever.
	q.err = nil
	rr := send()
	if rr.Code != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202", rr.Code)
	}
	if q.count() != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", q.count())
	}
	received, _ := store.ListEnvelopes(context.Background(), InboundStatusReceived, 0)
	if len(received) != 1 {
		t.Fatalf("received envelopes = %d, want 1", len(received))
	}
}

// failingUpdateStore wraps an InboundStore so UpdateEnvelope always errors —
// the DB-pressure-during-queue-outage coincidence where the enqueue-failure
// cleanup itself cannot land.
type failingUpdateStore struct {
	InboundStore
}

func (s *failingUpdateStore) UpdateEnvelope(context.Context, InboundEnvelope) error {
	return errors.New("db update down")
}

func TestRedeliverySurvivesFailedCleanup(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{}`)
	store := &failingUpdateStore{InboundStore: NewMemoryInboundStore()}
	q := &stubQueue{err: errors.New("queue down")}
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    store,
		Queue:    q,
		JobType:  "webhook.inbound",
		DedupeKeyFunc: func(r *http.Request, _ []byte) string {
			return r.Header.Get("X-GitHub-Delivery")
		},
	})

	send := func() *httptest.ResponseRecorder {
		headers := signedHeaders(body, secret)
		headers["X-GitHub-Delivery"] = "abc-123"
		req := newIngestRequest(http.MethodPost, string(body), headers)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	// First delivery: enqueue fails AND the cleanup update fails → 500.
	if rr := send(); rr.Code != http.StatusInternalServerError {
		t.Fatalf("first delivery status = %d, want 500", rr.Code)
	}

	// The retry must still be re-ingested: a never-enqueued envelope may
	// not dedupe-ack the redelivery no matter which cleanup step failed.
	q.err = nil
	rr := send()
	if rr.Code != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202 (event lost)", rr.Code)
	}
	if q.count() != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", q.count())
	}
}

func TestIngestHandler_Validation(t *testing.T) {
	mk := func(c IngestConfig) (http.Handler, error) {
		return IngestHandler(c)
	}
	v := TimestampedVerifier("x", time.Minute)
	st := NewMemoryInboundStore()
	q := &stubQueue{}
	cases := []struct {
		name string
		cfg  IngestConfig
	}{
		{"missing source", IngestConfig{Verifier: v, Store: st}},
		{"missing verifier", IngestConfig{Source: "s", Store: st}},
		{"missing store", IngestConfig{Source: "s", Verifier: v}},
		{"queue without jobtype", IngestConfig{Source: "s", Verifier: v, Store: st, Queue: q}},
	}
	for _, c := range cases {
		if _, err := mk(c.cfg); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
	// Valid config with queue succeeds.
	if _, err := mk(IngestConfig{Source: "s", Verifier: v, Store: st, Queue: q, JobType: "jt"}); err != nil {
		t.Errorf("valid cfg: %v", err)
	}
}

// ----- verifier unit tests --------------------------------------------------

func TestTimestampedVerifier_AcceptsFresh(t *testing.T) {
	body := []byte(`{"a":1}`)
	v := TimestampedVerifier("sek", 5*time.Minute)
	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		SignatureHeader: SignWithTimestamp("sek", time.Now().Unix(), body),
	})
	if err := v(req, body); err != nil {
		t.Errorf("fresh signature rejected: %v", err)
	}
}

func TestTimestampedVerifier_RejectsStale(t *testing.T) {
	body := []byte(`{"a":1}`)
	v := TimestampedVerifier("sek", 5*time.Minute)
	stale := time.Now().Add(-2 * time.Hour).Unix()
	req := newIngestRequest(http.MethodPost, string(body), map[string]string{
		SignatureHeader: SignWithTimestamp("sek", stale, body),
	})
	if err := v(req, body); err == nil {
		t.Errorf("stale signature accepted")
	}
}

func TestHMACSHA256Verifier(t *testing.T) {
	body := []byte(`{"push":true}`)
	const header, prefix, secret = "X-Hub-Signature-256", "sha256=", "topsecret"

	cases := []struct {
		name   string
		header string
		value  string
		wantOK bool
	}{
		{"valid", header, ghSignature(body, secret), true},
		{"invalid hex", header, prefix + "deadbeef", false},
		{"wrong prefix", header, "sha1=" + strings.TrimPrefix(ghSignature(body, secret), "sha256="), false},
		{"missing header", "", "", false},
		{"empty header", header, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hdrs := map[string]string{}
			if c.header != "" {
				hdrs[c.header] = c.value
			}
			req := newIngestRequest(http.MethodPost, string(body), hdrs)
			v := HMACSHA256Verifier(header, prefix, secret)
			err := v(req, body)
			if c.wantOK && err != nil {
				t.Errorf("expected accept, got %v", err)
			}
			if !c.wantOK && err == nil {
				t.Errorf("expected reject, got accept")
			}
		})
	}
}

// ----- ProcessInbound tests -------------------------------------------------

func TestProcessInbound_Success(t *testing.T) {
	store := NewMemoryInboundStore()
	env := InboundEnvelope{
		ID: "env-1", Source: "github", Status: InboundStatusReceived,
		Payload: []byte(`{}`), ReceivedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.AddEnvelope(context.Background(), env)

	called := false
	h := ProcessInbound(store, func(_ context.Context, e InboundEnvelope) error {
		called = true
		if e.ID != "env-1" {
			t.Errorf("handler got id %q", e.ID)
		}
		return nil
	})
	payload, _ := json.Marshal(map[string]string{"envelope_id": "env-1", "source": "github"})
	if err := h(context.Background(), queue.Job{Type: "webhook.inbound", Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Errorf("business handler not called")
	}
	got, _ := store.GetEnvelope(context.Background(), "env-1")
	if got.Status != InboundStatusProcessed {
		t.Errorf("status = %q, want processed", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", got.Attempts)
	}
}

func TestProcessInbound_Failure(t *testing.T) {
	store := NewMemoryInboundStore()
	env := InboundEnvelope{
		ID: "env-2", Source: "github", Status: InboundStatusReceived,
		Payload: []byte(`{}`), ReceivedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.AddEnvelope(context.Background(), env)

	boom := errors.New("downstream exploded")
	h := ProcessInbound(store, func(_ context.Context, _ InboundEnvelope) error {
		return boom
	})
	payload, _ := json.Marshal(map[string]string{"envelope_id": "env-2"})
	err := h(context.Background(), queue.Job{Payload: payload})
	if !errors.Is(err, boom) {
		t.Errorf("returned err = %v, want %v", err, boom)
	}
	got, _ := store.GetEnvelope(context.Background(), "env-2")
	if got.Status != InboundStatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", got.Attempts)
	}
	if got.LastError != boom.Error() {
		t.Errorf("last error = %q, want %q", got.LastError, boom.Error())
	}
}

func TestProcessInbound_MissingEnvelope(t *testing.T) {
	store := NewMemoryInboundStore()
	h := ProcessInbound(store, func(context.Context, InboundEnvelope) error { return nil })
	payload, _ := json.Marshal(map[string]string{"envelope_id": "nope"})
	if err := h(context.Background(), queue.Job{Payload: payload}); err == nil {
		t.Errorf("expected error for missing envelope")
	}
}

// TestProcessInbound_AgainstRealQueue wires ProcessInbound into a live
// MemoryQueue to confirm the adapter works end-to-end through Enqueue →
// worker → handler, not just via a direct call.
func TestProcessInbound_AgainstRealQueue(t *testing.T) {
	store := NewMemoryInboundStore()
	env := InboundEnvelope{
		ID: "env-3", Source: "github", Status: InboundStatusReceived,
		Payload: []byte(`{"k":"v"}`), ReceivedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.AddEnvelope(context.Background(), env)

	q := queue.NewMemoryQueue(1)
	q.RegisterHandler("webhook.inbound", ProcessInbound(store, func(_ context.Context, e InboundEnvelope) error {
		return nil
	}))
	q.Start()
	defer q.Close()

	payload, _ := json.Marshal(map[string]string{"envelope_id": "env-3", "source": "github"})
	if err := q.Enqueue(context.Background(), queue.Job{Type: "webhook.inbound", Payload: payload}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.GetEnvelope(context.Background(), "env-3")
		if got != nil && got.Status == InboundStatusProcessed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("envelope never reached processed")
}

// ----- misc -----------------------------------------------------------------

// TestIngest_SignatureRequiresReadingBody guards against a regression where
// the verifier reads r.Body before the handler does: the handler must read
// the body exactly once and pass those bytes to the verifier.
func TestIngest_BodyReadOnceForVerify(t *testing.T) {
	const secret = "s3cr3t"
	body := []byte(`{"once":true}`)
	h, _ := IngestHandler(IngestConfig{
		Source:   "github",
		Verifier: TimestampedVerifier(secret, 5*time.Minute),
		Store:    NewMemoryInboundStore(),
	})
	req := newIngestRequest(http.MethodPost, string(body), signedHeaders(body, secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; verifier/body ordering broke", rr.Code)
	}
}
