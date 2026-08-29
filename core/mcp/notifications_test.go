package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// resourceSubsSnapshot copies the per-uri subscription counts for
// assertions.
func resourceSubsSnapshot(s *Server) map[string]int {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	out := make(map[string]int, len(s.resourceSubs))
	for uri, n := range s.resourceSubs {
		out[uri] = n
	}
	return out
}

// resources/subscribe pins caller-controlled strings in server state.
// The uri cap keeps that bounded; oversize is invalid params, never a
// truncation (the uri is a map key — truncating it would arm updates
// for the wrong uri).
func TestSubscribeRejectsOversizedURI(t *testing.T) {
	s := NewServer()
	oversize := "x://" + strings.Repeat("a", maxResourceURIBytes)
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "resources/subscribe",
		Params: json.RawMessage(`{"uri":"` + oversize + `"}`),
	})
	if resp.Error == nil {
		t.Error("SECURITY: [resource-exhaustion] oversize uri accepted; want invalid params")
	}
	if got := resourceSubsSnapshot(s); len(got) != 0 {
		t.Errorf("oversize uri pinned state: %v", got)
	}
	// The boundary is inclusive: a uri of exactly maxResourceURIBytes
	// bytes passes.
	atCap := "x://" + strings.Repeat("b", maxResourceURIBytes-4)
	resp = s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 2, Method: "resources/subscribe",
		Params: json.RawMessage(`{"uri":"` + atCap + `"}`),
	})
	if resp.Error != nil {
		t.Errorf("uri at the cap refused: %v", resp.Error)
	}
}

// The distinct-uri cap bounds the retained subscription map. Refcounts
// do not count toward it: re-subscribing an existing uri must keep
// working at the cap, a refused subscribe pins nothing, and
// unsubscribing a uri to zero frees a slot.
func TestSubscribeCapsDistinctURIs(t *testing.T) {
	s := NewServer()
	for i := range maxResourceSubscriptions {
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: i, Method: "resources/subscribe",
			Params: json.RawMessage(fmt.Sprintf(`{"uri":"x://%d"}`, i)),
		})
		if resp.Error != nil {
			t.Fatalf("subscribe %d under the cap failed: %v", i, resp.Error)
		}
	}
	subscribe := func(uri string, id int) Response {
		return s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: id, Method: "resources/subscribe",
			Params: json.RawMessage(`{"uri":"` + uri + `"}`),
		})
	}
	if resp := subscribe("x://0", maxResourceSubscriptions+1); resp.Error != nil {
		t.Errorf("re-subscribe to an existing uri refused at the cap: %v", resp.Error)
	}
	if resp := subscribe("x://extra", maxResourceSubscriptions+2); resp.Error == nil {
		t.Error("SECURITY: [resource-exhaustion] distinct-uri cap not enforced")
	}
	if _, ok := resourceSubsSnapshot(s)["x://extra"]; ok {
		t.Error("refused subscribe pinned state anyway")
	}
	// x://0 holds two counts (initial + re-subscribe): release both to
	// free the entry.
	for id := range 2 {
		if resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: maxResourceSubscriptions + 10 + id, Method: "resources/unsubscribe",
			Params: json.RawMessage(`{"uri":"x://0"}`),
		}); resp.Error != nil {
			t.Fatalf("unsubscribe errored: %v", resp.Error)
		}
	}
	if resp := subscribe("x://freed", maxResourceSubscriptions+20); resp.Error != nil {
		t.Errorf("subscribe after unsubscribe refused (slot not freed): %v", resp.Error)
	}
}

// The last stream's disconnect drops the retained resource
// subscriptions: with no stream there is nobody to deliver to, and the
// per-uri refcount cannot be attributed to the departing connection
// (this transport has no session correlation), so the whole map goes.
func TestLastDisconnectClearsResourceSubscriptions(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	waitForSubscribers(t, s, 1)

	for _, uri := range []string{"docs://a", "docs://b"} {
		if r := postMCP(t, ts, false, `{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"`+uri+`"}}`); r.Error != nil {
			t.Fatalf("resources/subscribe %s errored: %v", uri, r.Error)
		}
	}
	if got := resourceSubsSnapshot(s); len(got) != 2 {
		t.Fatalf("subscriptions not armed: %v", got)
	}

	resp.Body.Close() // the last (only) stream goes away

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sseRegistryCount(s) == 0 {
			if got := resourceSubsSnapshot(s); len(got) != 0 {
				t.Errorf("subscriptions survived the last disconnect: %v (unbounded retained state)", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("subscriber registry never emptied after disconnect")
}

// blockingWriter simulates a client that stopped reading: the writes
// of a message event block until the handler has armed a write
// deadline, then fail as if the deadline had expired. Handshake writes
// flow so the subscriber registers first, which is what makes the
// unregistration assertion meaningful. If the handler never arms a
// deadline, the blocked write never returns and the test's watchdog
// fires — that hang is the regression the deadline fixes.
type blockingWriter struct {
	unblock chan struct{}
	once    sync.Once
}

func (b *blockingWriter) Header() http.Header { return http.Header{} }
func (b *blockingWriter) WriteHeader(int)     {}

func (b *blockingWriter) Write(p []byte) (int, error) {
	if !strings.Contains(string(p), "event: message") {
		return len(p), nil // handshake and framing writes flow
	}
	<-b.unblock
	return 0, fmt.Errorf("write: i/o timeout")
}

func (b *blockingWriter) Flush() {}

func (b *blockingWriter) SetWriteDeadline(deadline time.Time) error {
	// Not the real 10s: the test proves the mechanism (deadline armed →
	// blocked write released → write fails → handler returns and
	// unregisters), not the duration. Release shortly after the arm.
	b.once.Do(func() {
		time.AfterFunc(50*time.Millisecond, func() { close(b.unblock) })
	})
	return nil
}

// A client that stops reading pins nothing: each write on the SSE
// notification stream carries a fresh deadline, so the handler returns
// instead of hanging on the blocked write, and its exit path
// unregisters the subscriber.
func TestBlockedWriteDeadlineReturnsAndUnregisters(t *testing.T) {
	s := NewServer()
	w := &blockingWriter{unblock: make(chan struct{})}
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	r.Header.Set("Accept", "text/event-stream")

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sseGetHandler(w, r)
	}()

	waitForSubscribers(t, s, 1)
	s.NotifyToolsListChanged() // a message event: its write blocks

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SECURITY: [liveness] handler hung on a write the client never reads")
	}
	if sseRegistryCount(s) != 0 {
		t.Errorf("subscriber still registered after the failed write: %d live", sseRegistryCount(s))
	}
}

// The list_changed a gated registration fires carries the item's own
// gate: a caller the gate refuses cannot see the item in its list
// method and must not be told it appeared. Concrete resources are the
// documented exception — their metadata stays listed; their gate
// guards the read, so RegisterResource is not covered here.
func TestGatedRegistrationWithholdsListChanged(t *testing.T) {
	s := NewServer()
	ts := newNotificationServer(t, s)
	authedEvents := openStream(t, ts, true)
	anonEvents := openStream(t, ts, false)
	waitForSubscribers(t, s, 2)

	mustRegisterGated(t, s, "hidden_tool", requireUser)
	method, uri := decodeNotification(t, awaitSSE(t, authedEvents, "tools list_changed on the allowed stream"))
	if method != "notifications/tools/list_changed" || uri != "" {
		t.Errorf("allowed stream got %q (uri %q), want payload-free tools list_changed", method, uri)
	}
	expectNoSSE(t, anonEvents, "tools list_changed for a caller the tool gate refuses")

	if err := s.RegisterResourceTemplate("hidden://tpl/{id}", "Hidden", "text/plain",
		WithResourceTemplateGate(requireUser)); err != nil {
		t.Fatal(err)
	}
	method, uri = decodeNotification(t, awaitSSE(t, authedEvents, "resources list_changed on the allowed stream"))
	if method != "notifications/resources/list_changed" || uri != "" {
		t.Errorf("allowed stream got %q (uri %q), want payload-free resources list_changed", method, uri)
	}
	expectNoSSE(t, anonEvents, "resources list_changed for a caller the template gate refuses")
}
