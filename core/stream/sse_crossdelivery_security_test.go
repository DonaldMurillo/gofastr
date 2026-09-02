package stream_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// streamLines forwards every raw SSE line from resp to ch and closes ch
// when the body ends. The channel is buffered so a test that stops
// reading cannot block the scanner goroutine forever.
func streamLines(resp *http.Response, ch chan<- string) {
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			ch <- sc.Text()
		}
		// A read error is the body closing under the test; the stream
		// simply ends, and closing ch is the signal the reader waits on.
		_ = sc.Err()
		close(ch)
	}()
}

// Property: fanout delivery is topic-scoped. An event published on one
// broker's topic must never reach a subscriber of a different topic on
// the same fanout backend, no matter how the topics are spelled.
func TestSSEFanoutTopicIsolation(t *testing.T) {
	f := fanout.NewInProcess()
	alpha := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "alpha", Fanout: f})
	beta := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "beta", Fanout: f})
	defer alpha.Close()
	defer beta.Close()

	srvBeta := httptest.NewServer(http.HandlerFunc(beta.Subscribe))
	defer srvBeta.Close()
	resp := connectSSE(t, srvBeta)
	defer resp.Body.Close()
	lines := make(chan string, 64)
	streamLines(resp, lines)
	time.Sleep(100 * time.Millisecond) // let the subscriber register

	alpha.Publish("msg", "alpha-secret")

	// The beta subscriber must stay silent for the alpha event.
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("subscription stream ended unexpectedly during the isolation window")
			}
			if strings.Contains(line, "alpha-secret") {
				t.Fatalf("SECURITY: [sse-fanout] event published on topic alpha reached a topic-beta subscriber: %q. Attack: cross-topic information disclosure via shared fanout backend.", line)
			}
		case <-deadline:
			// silence is the pass condition
		}
		break
	}

	// Control: the same subscriber does receive same-topic events, so
	// the silence above is isolation, not a dead subscription.
	beta.Publish("msg", "beta-hello")
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("subscription stream ended before the control event arrived")
			}
			if strings.Contains(line, "beta-hello") {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the same-topic control event (subscription never worked)")
		}
	}
}

// Property: cross-replica delivery preserves the event envelope. A
// subscriber on the peer replica must see the event's name and id, not
// just its data — LastEventID resume across replicas depends on both.
func TestSSEFanoutPreservesNameAndID(t *testing.T) {
	f := fanout.NewInProcess()
	origin := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	peer := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
	defer origin.Close()
	defer peer.Close()

	srvPeer := httptest.NewServer(http.HandlerFunc(peer.Subscribe))
	defer srvPeer.Close()
	resp := connectSSE(t, srvPeer)
	defer resp.Body.Close()
	lines := make(chan string, 64)
	streamLines(resp, lines)
	time.Sleep(100 * time.Millisecond)

	origin.Publish("tick", "cross-body", "evt-77")

	want := map[string]bool{
		"event: tick":      false,
		"id: evt-77":       false,
		"data: cross-body": false,
	}
	timeout := time.After(2 * time.Second)
	for {
		complete := true
		for _, seen := range want {
			if !seen {
				complete = false
			}
		}
		if complete {
			return
		}
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream ended before the full envelope arrived; seen so far: %v", want)
			}
			if _, tracked := want[line]; tracked {
				want[line] = true
			}
		case <-timeout:
			t.Fatalf("timed out waiting for the cross-replica envelope; seen so far: %v", want)
		}
	}
}

// Property: an event envelope carrying SSE field-break bytes (CR/LF/NUL
// in the event name or id) cannot forge extra SSE fields on the
// subscriber's stream. This is the same property TestSSE_EventNameInjection
// pins on SSEWriter directly, asserted here on the whole delivery path —
// both lanes a subscriber's bytes can arrive on: local Publish and a
// cross-replica fanout hop.
func TestSSEHostileEnvelopeSanitized(t *testing.T) {
	publish := func(b *stream.SSEBroker) {
		// name, id, and data each attempt to terminate their field and
		// open forged ones; the data CR attempts a WHATWG-parser split.
		b.Publish("evt\ndata: forged", "body\r\nretry: 999", "42\nretry: 0")
	}

	// collectUntil drains lines until the event's last expected data
	// line arrives, then hands the accumulated lines back.
	collectUntil := func(t *testing.T, lines <-chan string) []string {
		t.Helper()
		var got []string
		timeout := time.After(2 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream ended before the hostile event arrived; got %q", got)
				}
				got = append(got, line)
				if line == "data: retry: 999" {
					return got
				}
			case <-timeout:
				t.Fatalf("timed out waiting for the hostile event; got %q", got)
			}
		}
	}

	assertClean := func(t *testing.T, got []string) {
		t.Helper()
		hasEvent, hasID := false, false
		for _, line := range got {
			if strings.HasPrefix(line, "retry:") {
				t.Fatalf("SECURITY: [sse-fanout] forged SSE field reached the subscriber stream: %q. Attack: reconnect-storm amplification via injected retry:.", line)
			}
			if line == "event: forged" || line == "data: forged" {
				t.Fatalf("SECURITY: [sse-fanout] forged SSE field reached the subscriber stream: %q. Attack: event-name/id injection mints client-side handlers the server never sent.", line)
			}
			if line == "event: evt" {
				hasEvent = true
			}
			if line == "id: 42" {
				hasID = true
			}
		}
		if !hasEvent || !hasID {
			t.Fatalf("sanitized envelope lost its legitimate fields: %q", got)
		}
	}

	t.Run("local publish lane", func(t *testing.T) {
		broker := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t"})
		defer broker.Close()
		srv := httptest.NewServer(http.HandlerFunc(broker.Subscribe))
		defer srv.Close()
		resp := connectSSE(t, srv)
		defer resp.Body.Close()
		lines := make(chan string, 64)
		streamLines(resp, lines)
		time.Sleep(100 * time.Millisecond)

		publish(broker)
		assertClean(t, collectUntil(t, lines))
	})

	t.Run("cross-replica lane", func(t *testing.T) {
		f := fanout.NewInProcess()
		origin := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
		peer := stream.NewSSEBroker(stream.SSEBrokerConfig{Topic: "t", Fanout: f})
		defer origin.Close()
		defer peer.Close()
		srv := httptest.NewServer(http.HandlerFunc(peer.Subscribe))
		defer srv.Close()
		resp := connectSSE(t, srv)
		defer resp.Body.Close()
		lines := make(chan string, 64)
		streamLines(resp, lines)
		time.Sleep(100 * time.Millisecond)

		publish(origin)
		assertClean(t, collectUntil(t, lines))
	})
}
