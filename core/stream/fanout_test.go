package stream_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// connectSSE subscribes a real HTTP client to broker served at srv and returns
// the response body reader. The caller closes resp.Body.
func connectSSE(t *testing.T, srv *httptest.Server) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET %s: %v", srv.URL, err)
	}
	return resp
}

// firstSSEData scans body for the first "data: " line and sends it on ch.
func firstSSEData(resp *http.Response, ch chan<- string) {
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			ch <- strings.TrimPrefix(line, "data: ")
			return
		}
	}
}

// TestSSEBrokerFanoutCrossDelivery: a client subscribed to broker B receives
// an event published on broker A (the other replica) via the shared fanout.
func TestSSEBrokerFanoutCrossDelivery(t *testing.T) {
	f := fanout.NewInProcess()
	brokerA := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	brokerB := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	defer brokerA.Close()
	defer brokerB.Close()

	srvB := httptest.NewServer(http.HandlerFunc(brokerB.Subscribe))
	defer srvB.Close()

	resp := connectSSE(t, srvB)
	defer resp.Body.Close()
	time.Sleep(100 * time.Millisecond) // let the subscriber register

	brokerA.Publish("msg", "hello-remote")

	dataCh := make(chan string, 1)
	go firstSSEData(resp, dataCh)
	select {
	case d := <-dataCh:
		if !strings.Contains(d, "hello-remote") {
			t.Errorf("data = %q, want hello-remote", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cross-replica SSE delivery")
	}
}

// TestSSEBrokerFanoutOwnNodeNoDup: a subscriber on broker A receives an event
// published on A exactly once — A does not echo its own publish back from the
// fanout.
func TestSSEBrokerFanoutOwnNodeNoDup(t *testing.T) {
	f := fanout.NewInProcess()
	brokerA := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	defer brokerA.Close()

	srvA := httptest.NewServer(http.HandlerFunc(brokerA.Subscribe))
	defer srvA.Close()

	resp := connectSSE(t, srvA)
	defer resp.Body.Close()
	time.Sleep(100 * time.Millisecond)

	brokerA.Publish("msg", "solo-event")

	dataCh := make(chan string, 2)
	go firstSSEData(resp, dataCh)
	select {
	case d := <-dataCh:
		if !strings.Contains(d, "solo-event") {
			t.Errorf("data = %q, want solo-event", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for local SSE delivery")
	}

	// Give any echo loop time to manifest a duplicate.
	select {
	case extra := <-dataCh:
		t.Fatalf("received a duplicate SSE event (own-node echo not dropped): %q", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestSSEBrokerFanoutNoFanoutStillWorks: without a configured fanout, Publish
// behaves exactly as before (local-only), and Close is a safe no-op.
func TestSSEBrokerFanoutNoFanoutStillWorks(t *testing.T) {
	broker := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t"}) // no Fanout
	srv := httptest.NewServer(http.HandlerFunc(broker.Subscribe))
	defer srv.Close()

	resp := connectSSE(t, srv)
	defer resp.Body.Close()
	time.Sleep(100 * time.Millisecond)

	broker.Publish("msg", "local-only")
	dataCh := make(chan string, 1)
	go firstSSEData(resp, dataCh)
	select {
	case d := <-dataCh:
		if !strings.Contains(d, "local-only") {
			t.Errorf("data = %q, want local-only", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local delivery failed without fanout")
	}

	// Close must be a safe no-op when no fanout is attached.
	broker.Close()
}

// TestSSEBrokerFanoutStopDetaches: after Close(), a publish on A no longer
// reaches B.
func TestSSEBrokerFanoutStopDetaches(t *testing.T) {
	f := fanout.NewInProcess()
	brokerA := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	brokerB := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	defer brokerB.Close()

	srvB := httptest.NewServer(http.HandlerFunc(brokerB.Subscribe))
	defer srvB.Close()
	resp := connectSSE(t, srvB)
	defer resp.Body.Close()
	time.Sleep(100 * time.Millisecond)

	// Confirm cross-delivery works before Close.
	brokerA.Publish("msg", "first")
	dataCh := make(chan string, 1)
	go firstSSEData(resp, dataCh)
	select {
	case <-dataCh:
	case <-time.After(2 * time.Second):
		t.Fatal("pre-close delivery missed")
	}

	brokerA.Close()
	// After Close, publishes on A must not cross to B.
	brokerA.Publish("msg", "second")
	// Start a fresh data reader on the still-open response; nothing should arrive.
	dataCh2 := make(chan string, 1)
	go firstSSEData(resp, dataCh2)
	select {
	case extra := <-dataCh2:
		t.Fatalf("B received event after A.Close(): %q", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// stalledFanout blocks Publish forever; Subscribe is a no-op. Reproduces a
// stalled backend (e.g. a hung Postgres) behind the publish path.
type stalledFanout struct{}

func (stalledFanout) Publish(ctx context.Context, _ string, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}
func (stalledFanout) Subscribe(string, func([]byte)) (func(), error) { return func() {}, nil }

func TestSSEBrokerPublishNonBlockingOnStall(t *testing.T) {
	b := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: stalledFanout{}})
	defer b.Close()
	done := make(chan struct{})
	go func() {
		// Publish is called from request/emit paths; the fanout mirror must
		// never wait on the backend.
		for i := 0; i < 50; i++ {
			b.Publish("ev", "data")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a stalled fanout backend")
	}
}
