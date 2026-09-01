package a2a

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// pushRecord records one delivery attempt.
type pushRecord struct {
	method        string
	contentType   string
	token         string
	authorization string
	body          []byte
	path          string
}

type pushRecorder struct {
	mu   sync.Mutex
	reqs []pushRecord
	srv  *httptest.Server
}

func newPushRecorder(t *testing.T, handler http.HandlerFunc) *pushRecorder {
	t.Helper()
	rec := &pushRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, pushRecord{
			method:        r.Method,
			contentType:   r.Header.Get("Content-Type"),
			token:         r.Header.Get(PushNotificationTokenHeader),
			authorization: r.Header.Get("Authorization"),
			body:          body,
			path:          r.URL.Path,
		})
		rec.mu.Unlock()
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func (p *pushRecorder) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reqs)
}

func (p *pushRecorder) wait(t *testing.T, n int) []pushRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		reqs := append([]pushRecord(nil), p.reqs...)
		p.mu.Unlock()
		if len(reqs) >= n {
			return reqs
		}
		if time.Now().After(deadline) {
			t.Fatalf("push receiver saw %d requests, want %d", len(reqs), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pushConfigParams builds a SendMessage params object carrying an
// inline push config, so the run's first event reaches the receiver.
func pushConfigParams(url, token string) map[string]any {
	return map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{"skill": "echo"},
		},
		"configuration": map[string]any{
			"taskPushNotificationConfig": map[string]any{
				"url":   url,
				"token": token,
				"authentication": map[string]any{
					"scheme":      "Bearer",
					"credentials": "sekrit",
				},
			},
		},
	}
}

// TestPushDeliveryHeadersAndBody pins the delivery contract: POST,
// application/json, token header, Authorization for Bearer, and a body
// that is a valid StreamResponse naming the task.
func TestPushDeliveryHeadersAndBody(t *testing.T) {
	rec := newPushRecorder(t, nil)
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Working(TextPart("working")); err != nil {
			return err
		}
		return tc.Complete(TextPart("done"))
	})
	status, e, raw := h.call("alice", MethodSendMessage, pushConfigParams(rec.srv.URL+"/hook", "tok-1"))
	if status != 200 || e.Error != nil {
		t.Fatalf("send: status=%d err=%+v body=%s", status, e.Error, raw)
	}
	var resp struct {
		Result SendMessageResponse `json:"result"`
	}
	_ = json.Unmarshal(raw, &resp)

	// WORKING and COMPLETED events both arrive.
	reqs := rec.wait(t, 2)
	first := reqs[0]
	if first.method != http.MethodPost {
		t.Fatalf("method = %s", first.method)
	}
	if first.contentType != "application/json" {
		t.Fatalf("content type = %s", first.contentType)
	}
	if first.token != "tok-1" {
		t.Fatalf("token header = %q", first.token)
	}
	if first.authorization != "Bearer sekrit" {
		t.Fatalf("authorization = %q", first.authorization)
	}
	var sr StreamResponse
	if err := json.Unmarshal(first.body, &sr); err != nil {
		t.Fatalf("body is not a StreamResponse: %v (%s)", err, first.body)
	}
	if sr.StatusUpdate == nil || sr.StatusUpdate.TaskID != resp.Result.Task.ID {
		t.Fatalf("body event = %+v, want statusUpdate for %s", sr, resp.Result.Task.ID)
	}
}

// TestPushLoopbackRefusedWithoutAllowPrivate pins the SSRF refusal: a
// loopback URL is rejected with -32602 unless AllowPrivate is set.
func TestPushLoopbackRefusedWithoutAllowPrivate(t *testing.T) {
	rec := newPushRecorder(t, nil)
	h := newHarness(t, nil) // AllowPrivate false
	task := h.send("alice")
	_, e, _ := h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{
		"taskId": task.ID,
		"url":    rec.srv.URL + "/hook",
	})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("err = %+v, want -32602 for loopback URL", e.Error)
	}
	// localhost by name is refused too
	_, e, _ = h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{
		"taskId": task.ID,
		"url":    "http://localhost:9999/hook",
	})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("err = %+v, want -32602 for localhost", e.Error)
	}
	// and the receiver never saw anything
	if rec.count() != 0 {
		t.Fatalf("refused config still delivered %d times", rec.count())
	}
}

// TestPushRedirectNotFollowed pins that a 3xx to another host never
// carries the notification (or its token) there.
func TestPushRedirectNotFollowed(t *testing.T) {
	target := newPushRecorder(t, nil)
	source := newPushRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.srv.URL+"/hook", http.StatusFound)
	})
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })

	_, e, raw := h.call("alice", MethodSendMessage, pushConfigParams(source.srv.URL+"/hook", "tok-1"))
	if e.Error != nil {
		t.Fatalf("send: %+v body=%s", e.Error, raw)
	}
	source.wait(t, 1)
	// Give the client every chance to (wrongly) follow.
	time.Sleep(100 * time.Millisecond)
	if target.count() != 0 {
		t.Fatalf("redirect was followed: target saw %d requests", target.count())
	}
}

// TestPushFailureNotRetried pins one-attempt delivery: a 500 receiver
// sees exactly one POST per event, no retry loop.
func TestPushFailureNotRetried(t *testing.T) {
	rec := newPushRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	_, e, raw := h.call("alice", MethodSendMessage, pushConfigParams(rec.srv.URL+"/hook", "tok-1"))
	if e.Error != nil {
		t.Fatalf("send: %+v body=%s", e.Error, raw)
	}
	rec.wait(t, 1)
	time.Sleep(150 * time.Millisecond)
	if n := rec.count(); n != 1 {
		t.Fatalf("receiver saw %d requests, want exactly 1 (no retry)", n)
	}
}

// TestPushDisabled32003 pins the Disable posture.
func TestPushDisabled32003(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.Disable = true })
	task := h.send("alice")
	_, e, _ := h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{
		"taskId": task.ID,
		"url":    "https://hooks.example.test/cb",
	})
	if e.Error == nil || e.Error.Code != CodePushNotificationNotSupported {
		t.Fatalf("err = %+v, want -32003", e.Error)
	}
	// and a config riding inside SendMessage is refused the same way
	_, e, _ = h.call("alice", MethodSendMessage, pushConfigParams("https://hooks.example.test/cb", "t"))
	if e.Error == nil || e.Error.Code != CodePushNotificationNotSupported {
		t.Fatalf("inline config err = %+v, want -32003", e.Error)
	}
}

// TestPushConfigMissingTaskAndConfig pins -32001 for a missing task
// and -32602 for a missing config id.
func TestPushConfigMissingTaskAndConfig(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	_, e, _ := h.call("alice", MethodGetTaskPushNotificationConfig, map[string]any{"taskId": "ghost", "id": "c1"})
	if e.Error == nil || e.Error.Code != CodeTaskNotFound {
		t.Fatalf("missing task: err = %+v, want -32001", e.Error)
	}
	task := h.send("alice")
	_, e, _ = h.call("alice", MethodGetTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": "ghost"})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("missing config: err = %+v, want -32602", e.Error)
	}
	if e.Error != nil && e.Error.Message != "push config not found" {
		t.Fatalf("message = %q", e.Error.Message)
	}
	_, e, _ = h.call("alice", MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": "ghost"})
	if e.Error == nil || e.Error.Code != CodeInvalidParams {
		t.Fatalf("delete missing config: err = %+v, want -32602", e.Error)
	}
}

// TestGuardedTransportRefusesLoopback pins the dial-time half of the
// SSRF posture: even past validation, a guarded transport refuses to
// CONNECT to a loopback address (the DNS-rebinding backstop).
func TestGuardedTransportRefusesLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	tr := guardedTransport(false)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := tr.DialContext(dialCtx, "tcp", ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("guarded transport dialed a loopback address")
	}
	// and the permissive posture does dial it
	open := guardedTransport(true)
	conn2, err := open.DialContext(dialCtx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("allowPrivate transport must dial loopback: %v", err)
	}
	_ = conn2.Close()
}
