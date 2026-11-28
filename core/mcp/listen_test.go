package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func listenBody(id, notifications string) string {
	return `{"jsonrpc":"2.0","id":` + id + `,"method":"subscriptions/listen","params":{` + modernMeta + `,"notifications":` + notifications + `}}`
}

type listenStream struct {
	resp   *http.Response
	lines  *bufio.Reader
	cancel context.CancelFunc
}

func openListen(t *testing.T, url, body string) *listenStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(ProtocolVersionHeader, "2026-07-28")
	req.Header.Set(MethodHeader, "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	ls := &listenStream{resp: resp, lines: bufio.NewReader(resp.Body), cancel: cancel}
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return ls
}

// next returns the JSON of the stream's next message event, skipping keep-alive comments.
func (ls *listenStream) next(t *testing.T) map[string]any {
	t.Helper()
	var data strings.Builder
	for {
		line, err := ls.lines.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "data: "):
			data.WriteString(strings.TrimPrefix(line, "data: "))
		case line == "" && data.Len() > 0:
			var m map[string]any
			if err := json.Unmarshal([]byte(data.String()), &m); err != nil {
				t.Fatalf("event %q: %v", data.String(), err)
			}
			return m
		}
	}
}

func subscriptionID(m map[string]any) any {
	p, _ := m["params"].(map[string]any)
	meta, _ := p["_meta"].(map[string]any)
	return meta[MetaSubscriptionID]
}

func listenServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := modernServer(t)
	if err := s.RegisterResource("r://b", "b", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "b"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.ServeSSE("/mcp"))
	t.Cleanup(srv.Close)
	return s, srv
}

// waitSubscribers blocks until the server holds n streams, so a notification sent next reaches the stream under test.
func waitSubscribers(t *testing.T, s *Server, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.sseMu.Lock()
		got := len(s.sseSubs)
		s.sseMu.Unlock()
		if got == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server never held %d streams", n)
}

// The listen stream opens with an acknowledgment naming the request id and the honored filter, then delivers only the
// notification types and resource URIs the client asked for, each tagged with that id. Two streams with disjoint
// filters see disjoint notifications from the same sends.
func TestListenFiltersAndTags(t *testing.T) {
	s, srv := listenServer(t)
	a := openListen(t, srv.URL, listenBody(`"sub-a"`, `{"toolsListChanged":true,"resourceSubscriptions":["r://a","r://a"]}`))
	if a.resp.StatusCode != http.StatusOK || a.resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("listen = %d %s, want a 200 event stream", a.resp.StatusCode, a.resp.Header.Get("Content-Type"))
	}
	ack := a.next(t)
	p, _ := ack["params"].(map[string]any)
	honored, _ := p["notifications"].(map[string]any)
	if ack["method"] != "notifications/subscriptions/acknowledged" || subscriptionID(ack) != "sub-a" ||
		honored["toolsListChanged"] != true || honored["promptsListChanged"] != nil ||
		len(honored["resourceSubscriptions"].([]any)) != 1 {
		t.Fatalf("ack = %v, want subscription sub-a honoring tools and one r://a", ack)
	}
	b := openListen(t, srv.URL, listenBody(`7`, `{"promptsListChanged":true,"resourceSubscriptions":["r://b"]}`))
	if ack := b.next(t); subscriptionID(ack) != float64(7) {
		t.Fatalf("ack = %v, want subscription 7", ack)
	}
	waitSubscribers(t, s, 2)

	s.NotifyToolsListChanged()
	s.NotifyPromptsListChanged()
	s.NotifyResourcesListChanged()
	s.NotifyResourceUpdated("r://b")
	s.NotifyResourceUpdated("r://a")

	expect := func(ls *listenStream, id any, method, uri string) {
		t.Helper()
		got := ls.next(t)
		gp, _ := got["params"].(map[string]any)
		if got["method"] != method || subscriptionID(got) != id || (uri != "" && gp["uri"] != uri) {
			t.Fatalf("delivered %v, want %s %s tagged %v", got, method, uri, id)
		}
	}
	expect(a, "sub-a", "notifications/tools/list_changed", "")
	expect(a, "sub-a", "notifications/resources/updated", "r://a")
	expect(b, float64(7), "notifications/prompts/list_changed", "")
	expect(b, float64(7), "notifications/resources/updated", "r://b")
}

// A resource's gate also hides its updates on a listen stream.
func TestListenHonorsResourceGate(t *testing.T) {
	s := modernServer(t)
	if err := s.RegisterResource("r://secret", "s", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "s"}, nil
	}, WithResourceGate(func(context.Context) error { return errors.New("no") })); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.ServeSSE("/mcp"))
	t.Cleanup(srv.Close)
	ls := openListen(t, srv.URL, listenBody(`1`, `{"toolsListChanged":true,"resourceSubscriptions":["r://secret"]}`))
	ls.next(t)
	waitSubscribers(t, s, 1)
	s.NotifyResourceUpdated("r://secret")
	s.NotifyToolsListChanged()
	if got := ls.next(t); got["method"] != "notifications/tools/list_changed" {
		t.Fatalf("delivered = %v, want the gated resource update skipped", got)
	}
}

// Closing a stream releases its subscriber seat and the resource URIs it held, and only those.
func TestListenReleasesOnClose(t *testing.T) {
	s, srv := listenServer(t)
	a := openListen(t, srv.URL, listenBody(`1`, `{"resourceSubscriptions":["r://a"]}`))
	a.next(t)
	b := openListen(t, srv.URL, listenBody(`2`, `{"resourceSubscriptions":["r://b"]}`))
	b.next(t)
	waitSubscribers(t, s, 2)
	held := func(uri string) int {
		s.sseMu.Lock()
		defer s.sseMu.Unlock()
		return s.listenSubs[uri]
	}
	if held("r://a") != 1 || held("r://b") != 1 {
		t.Fatalf("held a=%d b=%d while listening, want 1 each", held("r://a"), held("r://b"))
	}
	a.cancel()
	waitSubscribers(t, s, 1)
	if held("r://a") != 0 || held("r://b") != 1 {
		t.Fatalf("after closing a: held a=%d b=%d, want 0 and 1", held("r://a"), held("r://b"))
	}
}

// Bad params and a refusing server gate answer a JSON-RPC error instead of opening a stream.
func TestListenRefusals(t *testing.T) {
	s, srv := listenServer(t)
	for _, tc := range []struct{ name, notifications string }{
		{"uris not an array", `{"resourceSubscriptions":"r://a"}`},
		{"empty uri", `{"resourceSubscriptions":[""]}`},
		{"flag not a bool", `{"toolsListChanged":"yes"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ls := openListen(t, srv.URL, listenBody(`1`, tc.notifications))
			var out map[string]any
			if err := json.NewDecoder(ls.resp.Body).Decode(&out); err != nil || errCode(out) != ErrInvalidParams {
				t.Fatalf("listen = %v (%v), want -32602", out, err)
			}
		})
	}
	s.SetGate(func(context.Context) error { return errors.New("no") })
	ls := openListen(t, srv.URL, listenBody(`1`, `{"toolsListChanged":true}`))
	var out map[string]any
	if err := json.NewDecoder(ls.resp.Body).Decode(&out); err != nil || out["error"] == nil {
		t.Fatalf("listen under a refusing gate = %v (%v), want an error", out, err)
	}
}

// Only ServeSSE holds listen streams: ServeHTTP and stdio answer -32601, and only ServeSSE's server/discover advertises
// listChanged and subscribe.
func TestListenOnlyOnStreamingTransport(t *testing.T) {
	s := modernServer(t)
	status, out := modernPOST(t, s, listenBody(`1`, `{"toolsListChanged":true}`),
		map[string]string{ProtocolVersionHeader: "2026-07-28", MethodHeader: "subscriptions/listen"})
	if status != http.StatusNotFound || errCode(out) != ErrMethodNotFound {
		t.Fatalf("ServeHTTP listen = %d %v, want 404 -32601", status, out)
	}
	if direct := handleJSON(t, s, listenBody(`1`, `{}`)); errCode(direct) != ErrMethodNotFound {
		t.Fatalf("HandleRequest listen = %v, want -32601", direct)
	}
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + modernMeta + `}}`
	hdr := map[string]string{ProtocolVersionHeader: "2026-07-28", MethodHeader: "server/discover"}
	caps := func(out map[string]any) map[string]any {
		r, _ := out["result"].(map[string]any)
		c, _ := r["capabilities"].(map[string]any)
		return c
	}
	_, plain := modernPOST(t, s, discover, hdr)
	if tools, _ := caps(plain)["tools"].(map[string]any); tools == nil || tools["listChanged"] != nil {
		t.Fatalf("ServeHTTP discover capabilities = %v, want tools without listChanged", caps(plain))
	}
	_, streaming := modernPOST(t, s.ServeSSE("/mcp"), discover, hdr)
	tools, _ := caps(streaming)["tools"].(map[string]any)
	res, _ := caps(streaming)["resources"].(map[string]any)
	if tools["listChanged"] != true || res["subscribe"] != true || res["listChanged"] != true {
		t.Fatalf("ServeSSE discover capabilities = %v, want listChanged and subscribe", caps(streaming))
	}
}

// subscriptions/listen passes the same envelope checks as every dispatched request: a non-scalar id and duplicate or
// case-folded params keys are refused before a stream opens.
func TestListenRefusesAmbiguousEnvelope(t *testing.T) {
	_, srv := listenServer(t)
	for _, tc := range []struct {
		name, body string
		code       float64
	}{
		{"object id", strings.Replace(listenBody(`1`, `{}`), `"id":1`, `"id":{"x":1}`, 1), ErrInvalidRequest},
		{"duplicate filter key", listenBody(`1`, `{"toolsListChanged":false,"toolsListChanged":true}`), ErrInvalidRequest},
		{"case-folded filter key", listenBody(`1`, `{"toolsListChanged":true,"TOOLSLISTCHANGED":true}`), ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ls := openListen(t, srv.URL, tc.body)
			var out map[string]any
			if err := json.NewDecoder(ls.resp.Body).Decode(&out); err != nil || errCode(out) != tc.code {
				t.Fatalf("listen = %v (%v), want %v", out, err, tc.code)
			}
		})
	}
}

// One caller's listen streams together hold at most maxResourceSubscriptionsPerCaller distinct URIs, as
// resources/subscribe does, so many streams cannot fill the server-wide URI table.
func TestListenCapsURIsPerCaller(t *testing.T) {
	s, srv := listenServer(t)
	uris := func(prefix string, n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `"` + prefix + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + "/" + strconv.Itoa(i) + `"`
		}
		return `{"resourceSubscriptions":[` + strings.Join(parts, ",") + `]}`
	}
	a := openListen(t, srv.URL, listenBody(`1`, uris("r://a/", maxResourceSubscriptionsPerCaller)))
	if ack := a.next(t); ack["method"] != "notifications/subscriptions/acknowledged" {
		t.Fatalf("first stream = %v, want an acknowledgment", ack)
	}
	waitSubscribers(t, s, 1)
	b := openListen(t, srv.URL, listenBody(`2`, `{"resourceSubscriptions":["r://one-more"]}`))
	var out map[string]any
	if err := json.NewDecoder(b.resp.Body).Decode(&out); err != nil || errCode(out) != ErrInvalidParams {
		t.Fatalf("second stream past the per-caller cap = %v (%v), want -32602", out, err)
	}
	same := openListen(t, srv.URL, listenBody(`3`, `{"resourceSubscriptions":["r://a/a/0"]}`))
	if ack := same.next(t); ack["method"] != "notifications/subscriptions/acknowledged" {
		t.Fatalf("a URI the caller already holds = %v, want it accepted", ack)
	}
}

func postLegacy(t *testing.T, url, body string) {
	t.Helper()
	resp, err := http.Post(url+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// A legacy resources/unsubscribe from the same caller cannot release what a listen stream holds: the stream keeps
// getting the URI's updates.
func TestListenSurvivesLegacyUnsubscribe(t *testing.T) {
	s, srv := listenServer(t)
	ls := openListen(t, srv.URL, listenBody(`1`, `{"resourceSubscriptions":["r://a"]}`))
	ls.next(t)
	waitSubscribers(t, s, 1)
	postLegacy(t, srv.URL, `{"jsonrpc":"2.0","id":9,"method":"resources/unsubscribe","params":{"uri":"r://a"}}`)
	postLegacy(t, srv.URL, `{"jsonrpc":"2.0","id":9,"method":"resources/unsubscribe","params":{"uri":"r://a"}}`)
	s.NotifyResourceUpdated("r://a")
	if got := ls.next(t); got["method"] != "notifications/resources/updated" {
		t.Fatalf("after a legacy unsubscribe the stream got %v, want the r://a update", got)
	}
}

// The per-caller cap counts listen and resources/subscribe URIs together, so neither kind can exceed it beside the other.
func TestListenAndLegacyShareCallerCap(t *testing.T) {
	s, srv := listenServer(t)
	parts := make([]string, maxResourceSubscriptionsPerCaller)
	for i := range parts {
		parts[i] = `"r://cap/` + strconv.Itoa(i) + `"`
	}
	ls := openListen(t, srv.URL, listenBody(`1`, `{"resourceSubscriptions":[`+strings.Join(parts, ",")+`]}`))
	ls.next(t)
	waitSubscribers(t, s, 1)
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.RemoteAddr = "127.0.0.1:1"
	ctx := WithRequest(context.Background(), r)
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "resources/subscribe", Params: json.RawMessage(`{"uri":"r://one-more"}`)})
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("legacy subscribe past the shared per-caller cap = %+v, want -32602", resp)
	}
	again := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "resources/subscribe", Params: json.RawMessage(`{"uri":"r://cap/0"}`)})
	if again.Error != nil {
		t.Fatalf("legacy subscribe to a URI the caller already listens to = %+v, want it accepted", again.Error)
	}
}

// Notifications a listen filter does not deliver never enter the stream's buffer, so a burst of them cannot drop it.
func TestListenFilterKeepsBufferFree(t *testing.T) {
	s := NewServer()
	f := &listenFilter{uris: map[string]bool{"r://a": true}}
	sub, ok := s.addFilteredSSESubscriber(context.Background(), "k", f)
	if !ok {
		t.Fatal("subscriber refused")
	}
	for range 4 * sseSubBufferSize {
		s.NotifyToolsListChanged()
	}
	s.sseMu.Lock()
	_, alive := s.sseSubs[sub]
	queued := len(sub.ch)
	s.sseMu.Unlock()
	if !alive || queued != 0 {
		t.Fatalf("after a burst of unrequested notifications: alive=%v queued=%d, want alive with an empty buffer", alive, queued)
	}
}

// When the server drops a listen stream (an evicted seat), it first answers the listen request with a complete result
// tagged with the subscription id, as the spec's graceful closure asks.
func TestListenGracefulClosure(t *testing.T) {
	s, srv := listenServer(t)
	s.SetSSESeatCap(1)
	s.SetSSESeatOverflow(SeatOverflowEvictOldest)
	first := openListen(t, srv.URL, listenBody(`"a"`, `{"toolsListChanged":true}`))
	first.next(t)
	waitSubscribers(t, s, 1)
	second := openListen(t, srv.URL, listenBody(`"b"`, `{"toolsListChanged":true}`))
	second.next(t)
	got := first.next(t)
	r, _ := got["result"].(map[string]any)
	meta, _ := r["_meta"].(map[string]any)
	if got["id"] != "a" || r["resultType"] != "complete" || meta[MetaSubscriptionID] != "a" {
		t.Fatalf("evicted stream's last message = %v, want the listen result for subscription a", got)
	}
}

// A listen refused at the seat cap answers 429 with a JSON-RPC error body, like its other refusals.
func TestListenSeatRefusalIsJSON(t *testing.T) {
	s, srv := listenServer(t)
	s.SetSSESeatCap(1)
	first := openListen(t, srv.URL, listenBody(`1`, `{"toolsListChanged":true}`))
	first.next(t)
	waitSubscribers(t, s, 1)
	second := openListen(t, srv.URL, listenBody(`2`, `{"toolsListChanged":true}`))
	var out map[string]any
	if second.resp.StatusCode != http.StatusTooManyRequests || json.NewDecoder(second.resp.Body).Decode(&out) != nil || errCode(out) != ErrTooManyStreams || out["id"] != float64(2) {
		t.Fatalf("seat refusal = %d %v, want 429 with -32000 under id 2", second.resp.StatusCode, out)
	}
}
