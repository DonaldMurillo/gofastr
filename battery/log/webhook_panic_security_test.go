package log

import (
	"net/http"
	"testing"
	"time"
)

type panicTransport struct{}

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("host transport exploded")
}

// TestWebhookClientPanicContained: WebhookOpts.HTTPClient is the host's
// client, and its Transport runs on the sink's worker goroutine, which no
// per-request recover net reaches. A panic there used to kill the
// process (and, had it not, leave Close waiting on a worker that never
// closed done). It is now one failed delivery: the batch is given up and
// the sink closes.
func TestWebhookClientPanicContained(t *testing.T) {
	s := WebhookSink("http://receiver.invalid/hook", WebhookOpts{
		BatchSize:     1,
		BatchInterval: time.Hour,
		HTTPClient:    &http.Client{Transport: panicTransport{}},
	})
	ws := s.(*webhookSink)
	ws.warnInterval = time.Hour // keep the give-up warning off the test output
	ws.lastWarnAt.Store(time.Now().UnixNano())
	if err := s.Write([]byte(`{"i":1}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for ws.GaveUp() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := ws.GaveUp(); got != 1 {
		t.Fatalf("GaveUp = %d, want 1 (the panicking delivery is a failed batch)", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
