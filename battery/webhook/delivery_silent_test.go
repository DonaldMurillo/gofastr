package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// errDeliveryUpdateStore delegates every Store method to *MemoryStore but
// fails UpdateDelivery — the DB-pressure case where the worker can't record
// a delivery's new state. Everything else (claim, read) still works.
type errDeliveryUpdateStore struct {
	*MemoryStore
}

func (errDeliveryUpdateStore) UpdateDelivery(context.Context, Delivery) error {
	return errors.New("db update down")
}

// safeLogBuf is a concurrency-safe sink for a Logger func.
type safeLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeLogBuf) logf(format string, args ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Mirror log.Printf: a format with no args is written verbatim so a '%'
	// in a literal message isn't treated as a verb.
	if len(args) == 0 {
		b.buf.WriteString(format)
	} else {
		b.buf.WriteString(fmt.Sprintf(format, args...))
	}
	b.buf.WriteByte('\n')
}

func (b *safeLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestManager_DeliveryUpdateFailureLogged pins fix #2 on the outbound
// Manager: when the store can't persist a delivery's post-attempt state, the
// failure must reach Options.Logger — otherwise a DB blip silently leaves the
// row at its pre-attempt status and the next tick re-delivers (duplicate) with
// zero signal. Pre-fix the five UpdateDelivery call sites discarded the error.
func TestManager_DeliveryUpdateFailureLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // success branch → UpdateDelivery(success)
	}))
	defer srv.Close()

	store := &errDeliveryUpdateStore{MemoryStore: NewMemoryStore()}
	var log safeLogBuf
	mgr := New(store, Options{
		MaxAttempts:          1,
		Backoff:              []time.Duration{0},
		PollInterval:         time.Hour, // don't race the worker — drive tick directly
		AllowPrivateNetworks: true,
		Logger:               log.logf,
	})
	// Deliberately NOT started: call tick synchronously for determinism.

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "x"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := mgr.Publish(ctx, "evt", []byte(`{}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	mgr.tick(ctx)

	if log.String() == "" {
		t.Fatalf("delivery-state update failure was not logged (silently discarded)")
	}
}

// failOnFailedEnvelopeStore succeeds for every state EXCEPT "failed": the
// processing-mark and processed-mark must land, but the post-handler-error
// "failed" write (inbound.go's discarded UpdateEnvelope) is the one under test.
type failOnFailedEnvelopeStore struct {
	envs map[string]InboundEnvelope
}

func (s *failOnFailedEnvelopeStore) AddEnvelope(_ context.Context, e InboundEnvelope) error {
	if s.envs == nil {
		s.envs = map[string]InboundEnvelope{}
	}
	s.envs[e.ID] = e
	return nil
}
func (s *failOnFailedEnvelopeStore) GetEnvelope(_ context.Context, id string) (*InboundEnvelope, error) {
	e, ok := s.envs[id]
	if !ok {
		return nil, nil
	}
	return &e, nil
}
func (s *failOnFailedEnvelopeStore) UpdateEnvelope(_ context.Context, e InboundEnvelope) error {
	if e.Status == InboundStatusFailed {
		return errors.New("db update down")
	}
	s.envs[e.ID] = e
	return nil
}
func (s *failOnFailedEnvelopeStore) ListEnvelopes(context.Context, string, int) ([]InboundEnvelope, error) {
	return nil, nil
}
func (s *failOnFailedEnvelopeStore) SeenDedupeKey(context.Context, string, string) (bool, error) {
	return false, nil
}

// TestProcessInbound_FailedStateUpdateLogged pins fix #2 on the inbound side:
// when the business handler errors AND the "failed"-state UpdateEnvelope is
// lost, the loss must be observable — the envelope is stranded "processing"
// with no LastError, invisible without a log. The handler's error is still
// returned (queue retries) regardless.
func TestProcessInbound_FailedStateUpdateLogged(t *testing.T) {
	store := &failOnFailedEnvelopeStore{}
	env := InboundEnvelope{
		ID: "env-x", Source: "github", Status: InboundStatusReceived,
		Payload: []byte(`{}`), ReceivedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.AddEnvelope(context.Background(), env)

	var log safeLogBuf
	boom := errors.New("downstream exploded")
	h := ProcessInbound(store, func(_ context.Context, _ InboundEnvelope) error {
		return boom
	}, WithProcessInboundLogger(log.logf))

	payload, _ := json.Marshal(map[string]string{"envelope_id": "env-x"})
	err := h(context.Background(), queue.Job{Payload: payload})
	if !errors.Is(err, boom) {
		t.Fatalf("returned err = %v, want the handler error so the queue retries", err)
	}
	if log.String() == "" {
		t.Fatalf("failed-state update failure was not logged (silently discarded)")
	}
}
