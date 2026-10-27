package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// hookQueue hands each enqueued job to onEnqueue before Enqueue returns,
// so a test can run the worker inside the ingest handler's window between
// the enqueue and the dedupe-key registration.
type hookQueue struct {
	stubQueue
	onEnqueue func(queue.Job)
}

func (q *hookQueue) Enqueue(ctx context.Context, job queue.Job) error {
	if err := q.stubQueue.Enqueue(ctx, job); err != nil {
		return err
	}
	q.onEnqueue(job)
	return nil
}

// ingestOnce posts one signed delivery carrying dedupe key "evt-1".
func ingestOnce(t *testing.T, s InboundStore, q queue.Queue) {
	t.Helper()
	h, err := IngestHandler(IngestConfig{
		Source: "github", Verifier: TimestampedVerifier("secret", 5*time.Minute),
		Store: s, Queue: q, JobType: "webhook.inbound",
		DedupeKeyFunc: func(*http.Request, []byte) string { return "evt-1" },
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"n":1}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newIngestRequest(http.MethodPost, body, signedHeaders([]byte(body), "secret")))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("ingest = %d %s", rec.Code, rec.Body)
	}
}

func assertKeyAndProcessed(t *testing.T, s InboundStore) {
	t.Helper()
	ctx := context.Background()
	if seen, err := s.SeenDedupeKey(ctx, "github", "evt-1"); err != nil || !seen {
		t.Fatalf("SeenDedupeKey = %v, %v; want true: the sender's redelivery would run the event twice", seen, err)
	}
	all, err := s.ListEnvelopes(ctx, "", 0)
	if err != nil || len(all) != 1 || all[0].Status != InboundStatusProcessed {
		t.Fatalf("envelopes = %+v, %v; want one processed", all, err)
	}
}

// A worker loads the envelope before the handler registers the key, then
// settles. The settle carries the worker's keyless snapshot and must not
// erase the key registered under it.
func TestDedupeKeySurvivesWorkerSettle(t *testing.T) {
	eachInboundStore(t, func(t *testing.T, s InboundStore) {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		worker := ProcessInbound(s, func(context.Context, InboundEnvelope) error {
			close(entered)
			<-release
			return nil
		})
		q := &hookQueue{onEnqueue: func(job queue.Job) {
			go func() {
				defer close(done)
				if err := worker(context.Background(), job); err != nil {
					t.Errorf("worker: %v", err)
				}
			}()
			<-entered
		}}
		ingestOnce(t, s, q)
		close(release)
		<-done
		assertKeyAndProcessed(t, s)
	})
}

// The worker finishes before the handler registers the key. Registration
// must still land on the processed row.
func TestDedupeKeyRegistersAfterProcessed(t *testing.T) {
	eachInboundStore(t, func(t *testing.T, s InboundStore) {
		worker := ProcessInbound(s, func(context.Context, InboundEnvelope) error { return nil })
		q := &hookQueue{onEnqueue: func(job queue.Job) {
			if err := worker(context.Background(), job); err != nil {
				t.Errorf("worker: %v", err)
			}
		}}
		ingestOnce(t, s, q)
		assertKeyAndProcessed(t, s)
	})
}
