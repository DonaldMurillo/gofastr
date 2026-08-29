package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamSSEEvents parses an SSE response body as it arrives and yields
// dispatched events on the returned channel (the streaming counterpart
// of parseSSEEvents). The channel closes when the body does.
func streamSSEEvents(t *testing.T, body io.Reader) <-chan sseEvent {
	t.Helper()
	out := make(chan sseEvent, 8)
	go func() {
		defer close(out)
		br := bufio.NewReader(body)
		var ev sseEvent
		var data []string
		dispatch := func() {
			if ev.event != "" || len(data) > 0 {
				out <- sseEvent{event: ev.event, data: strings.Join(data, "\n")}
			}
			ev, data = sseEvent{}, nil
		}
		for {
			line, err := br.ReadString('\n')
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				dispatch()
			case strings.HasPrefix(trimmed, "event:"):
				ev.event = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
			}
			if err != nil {
				dispatch()
				return
			}
		}
	}()
	return out
}

// awaitSSE waits for the next dispatched event, failing the test on
// stream close or timeout.
func awaitSSE(t *testing.T, events <-chan sseEvent, what string) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("stream closed while waiting for %s", what)
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return sseEvent{}
	}
}

// expectNoSSE asserts that nothing arrives on the stream within the
// window. The window only guards against a would-be delivery racing the
// assertion; the property itself (nothing was ever written) is
// deterministic.
func expectNoSSE(t *testing.T, events <-chan sseEvent, what string) {
	t.Helper()
	select {
	case ev, ok := <-events:
		if ok {
			t.Errorf("SECURITY: [disclosure] unexpected %s: %+v", what, ev)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// openStream opens one GET notification stream against ts and returns
// its event channel after swallowing the initial endpoint event. When
// auth is set the request carries an Authorization header, which the
// wrapped test server resolves into the principal the gates check.
func openStream(t *testing.T, ts *httptest.Server, auth bool) <-chan sseEvent {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if auth {
		req.Header.Set("Authorization", "Bearer t")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("GET stream status = %d", resp.StatusCode)
	}
	// Cleanup order matters: t.Cleanup runs LIFO, so the body closes
	// before the test server does (registered first by the caller) —
	// ts.Close would otherwise wait forever on the held connection.
	t.Cleanup(func() { resp.Body.Close() })
	events := streamSSEEvents(t, resp.Body)
	ev := awaitSSE(t, events, "initial endpoint event")
	if ev.event != "endpoint" {
		t.Errorf("first event = %q, want endpoint", ev.event)
	}
	return events
}

// postMCP sends one JSON-RPC POST and decodes the response. auth sets
// the Authorization header the wrapped test server resolves.
func postMCP(t *testing.T, ts *httptest.Server, auth bool, body string) Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer t")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding POST response: %v", err)
	}
	return out
}

// decodeNotification unpacks a message event into its JSON-RPC method
// and, when present, its params uri.
func decodeNotification(t *testing.T, ev sseEvent) (method, uri string) {
	t.Helper()
	if ev.event != "message" {
		t.Fatalf("event name = %q, want message", ev.event)
	}
	var msg struct {
		Method string `json:"method"`
		Params struct {
			URI string `json:"uri"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(ev.data), &msg); err != nil {
		t.Fatalf("data is not a JSON-RPC notification: %v (%q)", err, ev.data)
	}
	return msg.Method, msg.Params.URI
}

// sseRegistryCount snapshots the live subscriber count.
func sseRegistryCount(s *Server) int {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	return len(s.sseSubs)
}

// waitForSubscribers polls until the registry holds want streams.
func waitForSubscribers(t *testing.T, s *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sseRegistryCount(s) == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("subscriber count never reached %d", want)
}

// newNotificationServer wraps an MCP SSE handler in the middleware a
// real app has: an Authorization header resolves into the caller
// identity the gates evaluate. Streams and POSTs sent without the
// header stay anonymous.
func newNotificationServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	h := s.ServeSSE("/mcp")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			r = r.WithContext(authed(r.Context()))
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// A registration after a stream connected must reach it as a
// list_changed notification, for every registry that fires one.
func TestRegisterFiresListChangedOnStream(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	steps := []struct {
		name     string
		register func()
		method   string
	}{
		{
			name:     "tool",
			register: func() { mustRegisterOpen(t, s, "late_tool") },
			method:   "notifications/tools/list_changed",
		},
		{
			name: "resource",
			register: func() {
				if err := s.RegisterResource("docs://late", "Late", "text/plain",
					func(context.Context) (ResourceContents, error) {
						return ResourceContents{Text: "x"}, nil
					}); err != nil {
					t.Errorf("register resource: %v", err)
				}
			},
			method: "notifications/resources/list_changed",
		},
		{
			name: "prompt",
			register: func() {
				if err := s.RegisterPrompt("late_prompt",
					func(context.Context, map[string]string) ([]PromptMessage, error) {
						return nil, nil
					}); err != nil {
					t.Errorf("register prompt: %v", err)
				}
			},
			method: "notifications/prompts/list_changed",
		},
		{
			// The spec folds templates under the `resources`
			// capability: a template registration fires the resources
			// list_changed, the only one that exists for that surface.
			name: "template",
			register: func() {
				if err := s.RegisterResourceTemplate("docs://late/{id}", "Late", "text/plain"); err != nil {
					t.Errorf("register template: %v", err)
				}
			},
			method: "notifications/resources/list_changed",
		},
	}
	for _, step := range steps {
		step.register()
		method, uri := decodeNotification(t, awaitSSE(t, events, step.name+" list_changed"))
		if method != step.method {
			t.Errorf("%s registration fired %q, want %q", step.name, method, step.method)
		}
		if uri != "" {
			t.Errorf("%s list_changed carried a uri payload (%q); list_changed is payload-free", step.name, uri)
		}
	}
}

// THE headline property. notifications/resources/updated carries a
// gated resource's URI: it must reach only the streams whose gate
// would let them read the resource. Broadcasting it would disclose the
// existence and URI of a gated resource — the same disclosure the list
// methods prevent by hiding gated items and paging the post-gate set.
func TestGatedResourceUpdatedRespectsGate(t *testing.T) {
	s := NewServer()
	if err := s.RegisterResource("secret://books", "Books", "text/csv",
		func(context.Context) (ResourceContents, error) {
			return ResourceContents{Text: "id,amount\n1,49.00"}, nil
		},
		WithResourceGate(requireUser)); err != nil {
		t.Fatal(err)
	}
	ts := newNotificationServer(t, s)

	authedEvents := openStream(t, ts, true)
	anonEvents := openStream(t, ts, false)
	waitForSubscribers(t, s, 2)

	// Arm updates for the uri as the caller allowed to read it.
	if resp := postMCP(t, ts, true, `{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"secret://books"}}`); resp.Error != nil {
		t.Fatalf("resources/subscribe errored: %v", resp.Error)
	}

	s.NotifyResourceUpdated("secret://books")

	method, uri := decodeNotification(t, awaitSSE(t, authedEvents, "gated update on the allowed stream"))
	if method != "notifications/resources/updated" {
		t.Errorf("allowed stream got %q, want notifications/resources/updated", method)
	}
	if uri != "secret://books" {
		t.Errorf("updated uri = %q, want secret://books", uri)
	}

	expectNoSSE(t, anonEvents, "gated resource update on a gate-refusing stream")
}

// A caller the server-wide gate refuses learns nothing at all — not
// even a payload-free list_changed, which is otherwise safe to
// broadcast. The stream may exist; it must stay silent.
func TestServerGateRefusedGetsNoNotifications(t *testing.T) {
	s := NewServer()
	s.SetGate(requireUser)
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, false) // anonymous: the gate refuses it
	waitForSubscribers(t, s, 1)

	mustRegisterOpen(t, s, "public_tool") // fires NotifyToolsListChanged
	s.NotifyToolsListChanged()            // and again, explicitly

	expectNoSSE(t, events, "any notification for a server-gate-refused caller")
}

// A subscriber whose buffer stays full is dropped and its stream
// closed; the publisher never blocks and every other subscriber still
// receives everything.
func TestStalledSubscriberDroppedNotBlocking(t *testing.T) {
	s := NewServer()
	stalled := s.addSSESubscriber(context.Background()) // never drained
	live := s.addSSESubscriber(context.Background())

	const sends = sseSubBufferSize + 5
	received := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range sends {
			s.NotifyToolsListChanged()
			// Keep live's buffer drained non-blockingly so only the
			// genuinely stalled subscriber can hit the full-buffer
			// branch, deterministically.
			for {
				select {
				case <-live.ch:
					received++
					continue
				default:
				}
				break
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SECURITY: [liveness] publisher blocked on a stalled subscriber")
	}
	// The drop closes the channel with the sseSubBufferSize
	// notifications already buffered: drain exactly those, then the
	// channel must read closed. A still-open (or still-delivering)
	// channel means no drop happened.
	for range sseSubBufferSize {
		select {
		case _, ok := <-stalled.ch:
			if !ok {
				t.Fatal("stalled channel closed before draining its buffer")
			}
		default:
			t.Fatal("stalled channel drained early; expected the full buffer")
		}
	}
	select {
	case _, ok := <-stalled.ch:
		if ok {
			t.Error("stalled subscriber kept receiving after the drop")
		}
	default:
		t.Error("stalled subscriber's stream was not closed")
	}
	// Drain the remainder and count: live got every notification.
	for {
		select {
		case <-live.ch:
			received++
			continue
		default:
		}
		break
	}
	if received != sends {
		t.Errorf("live subscriber received %d of %d notifications", received, sends)
	}
}

// The client going away unregisters its stream: no map entry, no
// leaked goroutine blocked on a dead connection.
func TestDisconnectUnregistersSubscriber(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	waitForSubscribers(t, s, 1)

	resp.Body.Close() // the client goes away

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sseRegistryCount(s) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("subscriber registry not empty after disconnect: %d entries (map/goroutine leak)", sseRegistryCount(s))
}

// resources/subscribe and resources/unsubscribe are data methods: the
// server-wide gate must cover them like every other one, or a caller
// refused wholesale could still arm update notifications.
func TestServerGateClosesResourceSubscribe(t *testing.T) {
	s := NewServer()
	s.SetGate(requireUser)

	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "resources/subscribe",
		Params: json.RawMessage(`{"uri":"x://y"}`),
	})
	if resp.Error == nil {
		t.Error("SECURITY: [authz] server gate did not cover resources/subscribe")
	}
	resp = s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 2, Method: "resources/unsubscribe",
		Params: json.RawMessage(`{"uri":"x://y"}`),
	})
	if resp.Error == nil {
		t.Error("SECURITY: [authz] server gate did not cover resources/unsubscribe")
	}

	// The caller the gate allows gets through, and a params-less
	// subscribe is an invalid-params error, not a panic or a success.
	resp = s.HandleRequest(authed(context.Background()), Request{
		JSONRPC: "2.0", ID: 3, Method: "resources/subscribe",
		Params: json.RawMessage(`{"uri":"x://y"}`),
	})
	if resp.Error != nil {
		t.Errorf("authenticated resources/subscribe refused: %v", resp.Error)
	}
	resp = s.HandleRequest(authed(context.Background()), Request{
		JSONRPC: "2.0", ID: 4, Method: "resources/subscribe",
	})
	if resp.Error == nil {
		t.Error("params-less resources/subscribe succeeded; want invalid params")
	}
}

// NotifyResourceUpdated is a no-op until a resources/subscribe is
// active for the uri, and unsubscribe ends delivery: the spec has
// servers sending updates only for subscribed resources.
func TestUpdatedRequiresActiveSubscription(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, false)
	waitForSubscribers(t, s, 1)

	s.NotifyResourceUpdated("docs://x")
	expectNoSSE(t, events, "update for a uri nobody subscribed to")

	if resp := postMCP(t, ts, false, `{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"docs://x"}}`); resp.Error != nil {
		t.Fatalf("resources/subscribe errored: %v", resp.Error)
	}
	s.NotifyResourceUpdated("docs://x")
	method, uri := decodeNotification(t, awaitSSE(t, events, "update after subscribe"))
	if method != "notifications/resources/updated" || uri != "docs://x" {
		t.Errorf("got %q uri %q, want notifications/resources/updated uri docs://x", method, uri)
	}

	if resp := postMCP(t, ts, false, `{"jsonrpc":"2.0","id":2,"method":"resources/unsubscribe","params":{"uri":"docs://x"}}`); resp.Error != nil {
		t.Fatalf("resources/unsubscribe errored: %v", resp.Error)
	}
	s.NotifyResourceUpdated("docs://x")
	expectNoSSE(t, events, "update after unsubscribe")
}

// The handshake must advertise what the server now does: listChanged
// on every list capability and subscribe on resources.
func TestInitializeAdvertisesNotifications(t *testing.T) {
	s := NewServer()
	if err := s.RegisterResource("x://y", "Y", "text/plain",
		func(context.Context) (ResourceContents, error) {
			return ResourceContents{Text: "x"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterPrompt("p",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			return nil, nil
		}); err != nil {
		t.Fatal(err)
	}
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize errored: %v", resp.Error)
	}
	blob, _ := json.Marshal(resp.Result)
	for _, want := range []string{`"listChanged":true`, `"subscribe":true`} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("capabilities missing %s: %s", want, blob)
		}
	}
	if strings.Contains(string(blob), "false") {
		t.Errorf("capabilities still advertise a false capability: %s", blob)
	}
}
