package a2a

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pins for the bounded-wait and credential rules the v0.79.0 review
// added. Each fails under the previous code.

// A credentialed config over plain http is refused at registration, and
// post refuses to send one that was stored before the rule existed.
func TestCredentialedPushOverHTTPRefused(t *testing.T) {
	h := newHarness(t, nil) // AllowPrivate false
	task := h.send("alice")
	// TEST-NET-3: a public literal, so no DNS and no loopback refusal.
	base := map[string]any{"taskId": task.ID, "url": "http://203.0.113.5/hook"}
	_, e, _ := h.call("alice", MethodCreateTaskPushNotificationConfig, base)
	if e.Error != nil {
		t.Fatalf("plain http without credentials must register: %+v", e.Error)
	}
	withToken := map[string]any{"taskId": task.ID, "url": "http://203.0.113.5/hook", "token": "s3cret"} // not-a-secret: test fixture
	_, e, _ = h.call("alice", MethodCreateTaskPushNotificationConfig, withToken)
	if e.Error == nil || e.Error.Code != CodeInvalidParams || !strings.Contains(e.Error.Message, "plain http") {
		t.Fatalf("token over http must be -32602 naming plain http, got %+v", e.Error)
	}
	withAuth := map[string]any{"taskId": task.ID, "url": "http://203.0.113.5/hook",
		"authentication": map[string]any{"scheme": "Bearer", "credentials": "abc"}}
	_, e, _ = h.call("alice", MethodCreateTaskPushNotificationConfig, withAuth)
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("Authorization over http must be -32602, got %+v", e.Error)
	}

	// The stored-config defence: post never dials.
	dialed := false
	p := newPusher(PushOptions{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		dialed = true
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})}}, nil)
	err := p.post(PushNotificationConfig{URL: "http://203.0.113.5/hook", Token: "s3cret"}, []byte("{}")) // not-a-secret: test fixture
	if err == nil || dialed {
		t.Fatalf("post must refuse credentials over http before dialing: err=%v dialed=%v", err, dialed)
	}
	if err := p.post(PushNotificationConfig{URL: "https://203.0.113.5/hook", Token: "s3cret"}, []byte("{}")); err != nil || !dialed { // not-a-secret: test fixture
		t.Fatalf("https with credentials must deliver: err=%v dialed=%v", err, dialed)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A push delivery carries a deadline on its request, independent of the
// client's own Timeout.
func TestPushRequestCarriesDeadline(t *testing.T) {
	var hadDeadline bool
	p := newPusher(PushOptions{AllowPrivate: true, Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_, hadDeadline = r.Context().Deadline()
		return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
	})}}, nil)
	if err := p.post(PushNotificationConfig{URL: "https://receiver.example/hook"}, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if !hadDeadline {
		t.Fatal("delivery request carried no deadline; a client without Timeout would hang on a stalled receiver")
	}
}

// blockingStore blocks every UpdateTask until the context ends: a wedged
// database. Everything else is the memory store.
type blockingStore struct {
	Store
	entered chan struct{}
}

func (b *blockingStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// A store call made on a run's behalf is bounded: a TaskContext mutation
// against a wedged store returns once storeOpTimeout elapses instead of
// pinning the handler forever.
func TestStoreCallOnRunIsBounded(t *testing.T) {
	prev := storeOpTimeout
	storeOpTimeout = 100 * time.Millisecond
	t.Cleanup(func() { storeOpTimeout = prev })

	bs := &blockingStore{Store: NewMemoryStore(), entered: make(chan struct{}, 1)}
	h := newHarness(t, func(c *Config) { c.Store = bs })
	workingErr := make(chan error, 1)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		err := tc.Working()
		workingErr <- err
		return err
	})
	go h.call("alice", MethodSendMessage, map[string]any{"message": map[string]any{
		"role": "ROLE_USER", "parts": []any{map[string]any{"text": "hi"}}, "metadata": map[string]any{"skill": "echo"},
	}})
	select {
	case <-bs.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("store never entered")
	}
	select {
	case err := <-workingErr:
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Working against a wedged store: err = %v, want a deadline error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Working blocked past storeOpTimeout on a wedged store")
	}
}

// A non-streaming SendMessage returns when the client hangs up while the
// handler is still running; the run itself carries on and completes.
func TestSendReturnsWhenClientHangsUp(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	// A server-side witness: the request handler's return, independent
	// of the client's own deadline. Under the previous code (an
	// unconditional wait on the run) the client still timed out, but
	// this channel would not close until release.
	returned := make(chan struct{})
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.srv.ServeHTTP(w, r)
		close(returned)
	}))
	t.Cleanup(front.Close)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		if err := tc.Working(); err != nil {
			return err
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return tc.Complete()
	})
	body := `{"jsonrpc":"2.0","id":"c1","method":"SendMessage","params":{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}}`
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, front.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("the request must fail on the client's own deadline while the handler blocks")
	}
	// The property: the server handler returned because the client
	// left, BEFORE the run was released.
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("handleSend did not return after the client hung up; it is still waiting on the run")
	}
	// The run is still alive and completes once released.
	_, e, _ := h.call("alice", MethodListTasks, map[string]any{"status": string(TaskStateWorking)})
	if e.Error != nil || !strings.Contains(string(e.Result), `"totalSize":1`) {
		t.Fatalf("the run must continue after the client left: %+v %s", e.Error, e.Result)
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, e, _ = h.call("alice", MethodListTasks, map[string]any{"status": string(TaskStateCompleted)})
		if strings.Contains(string(e.Result), `"totalSize":1`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("released run never completed")
}

// deadlineRecorder is a ResponseRecorder that also accepts write
// deadlines, the way a real net/http response does.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines int
}

func (d *deadlineRecorder) SetWriteDeadline(time.Time) error { d.deadlines++; return nil }

// The SSE keep-alive comment sets a write deadline like a data event, so
// a client that stops reading cannot pin the stream on an idle tick.
func TestKeepAliveSetsWriteDeadline(t *testing.T) {
	h := newHarness(t, nil)
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	st := h.srv.newSSEStream(rec, []byte(`"1"`))
	if err := st.keepAlive(); err != nil {
		t.Fatal(err)
	}
	if rec.deadlines != 1 {
		t.Fatalf("keep-alive set %d write deadlines, want 1", rec.deadlines)
	}
	if !strings.Contains(rec.Body.String(), ": keep-alive") {
		t.Fatalf("keep-alive comment missing: %q", rec.Body.String())
	}
}
