package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// t0 is the fixed clock every deterministic test runs under.
var t0 = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

func ts(at time.Time) *Timestamp { return &Timestamp{at} }

// idGen hands out "gen1", "gen2", … in call order, so golden frames can
// name the exact ids the server minted.
type idGen struct{ n atomic.Int64 }

func (g *idGen) next() string { return fmt.Sprintf("gen%d", g.n.Add(1)) }

// harness wires a Server with a fixed clock, deterministic ids, an
// X-Owner header principal, one "echo" skill, and a quiet logger.
type harness struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	ids *idGen

	mu      sync.Mutex
	handler Handler
}

// echoHandler is the default skill: complete with "done".
func echoHandler(_ context.Context, t TaskContext) error {
	return t.Complete(TextPart("done"))
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	h := &harness{t: t, ids: &idGen{}, handler: echoHandler}
	cfg := Config{
		Skills: []Skill{{
			ID:          "echo",
			Name:        "Echo",
			Description: "echo the work",
			Tags:        []string{"test"},
			Handler:     func(ctx context.Context, tc TaskContext) error { return h.current()(ctx, tc) },
		}},
		Owner: func(r *http.Request) (string, bool) {
			if o := r.Header.Get("X-Owner"); o != "" {
				if o == "refuse" {
					return "", false
				}
				return o, true
			}
			return "alice", true
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.now = func() time.Time { return t0 }
	srv.newID = h.ids.next
	srv.keepAlive = 20 * time.Millisecond // keep-alives observable, not slow
	h.srv = srv
	h.ts = httptest.NewServer(srv)
	t.Cleanup(h.ts.Close)
	return h
}

func (h *harness) setHandler(f Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handler = f
}

func (h *harness) current() Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handler
}

// env is the parsed JSON-RPC response envelope.
type env struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
}

// call posts one JSON-RPC request and returns the HTTP status plus the
// parsed envelope. id is always "call-N" so goldens can echo it.
func (h *harness) call(owner, method string, params any) (int, *env, []byte) {
	h.t.Helper()
	return h.callID(owner, method, params, "call-1")
}

func (h *harness) callID(owner, method string, params any, id string) (int, *env, []byte) {
	h.t.Helper()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if params == nil {
		reqBody["params"] = struct{}{}
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		h.t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(b))
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if owner != "" {
		req.Header.Set("X-Owner", owner)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("call %s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read %s response: %v", method, err)
	}
	var e env
	if err := json.Unmarshal(raw, &e); err != nil {
		h.t.Fatalf("parse %s response %s: %v", method, raw, err)
	}
	return resp.StatusCode, &e, raw
}

// send posts a SendMessage with a text part routed by metadata and
// returns the task in the result.
func (h *harness) send(owner string, msgs ...any) *Task {
	h.t.Helper()
	params := map[string]any{
		"message": map[string]any{
			"role":  "ROLE_USER",
			"parts": []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{
				"skill": "echo",
			},
		},
	}
	for _, m := range msgs {
		if cfg, ok := m.(map[string]any); ok {
			params["configuration"] = cfg
		}
	}
	status, e, raw := h.call(owner, MethodSendMessage, params)
	if status != 200 || e.Error != nil {
		h.t.Fatalf("SendMessage: status=%d err=%+v body=%s", status, e.Error, raw)
	}
	var out SendMessageResponse
	if err := json.Unmarshal(e.Result, &out); err != nil {
		h.t.Fatalf("SendMessage result %s: %v", e.Result, err)
	}
	if out.Task == nil {
		h.t.Fatalf("SendMessage result has no task: %s", e.Result)
	}
	return out.Task
}

// sendWithTask posts a SendMessage addressed to an existing task (the
// resume path).
func (h *harness) sendWithTask(owner, taskID, text string) *Task {
	h.t.Helper()
	params := map[string]any{
		"message": map[string]any{
			"taskId": taskID,
			"role":   "ROLE_USER",
			"parts":  []any{map[string]any{"text": text}},
		},
	}
	status, e, raw := h.call(owner, MethodSendMessage, params)
	if status != 200 || e.Error != nil {
		h.t.Fatalf("SendMessage(resume): status=%d err=%+v body=%s", status, e.Error, raw)
	}
	var out SendMessageResponse
	if err := json.Unmarshal(e.Result, &out); err != nil || out.Task == nil {
		h.t.Fatalf("SendMessage(resume) result %s: %v", e.Result, err)
	}
	return out.Task
}

// assertJSON compares got against the canonical re-marshal of want.
func assertJSON(t *testing.T, what string, got []byte, want any) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: response is not JSON: %s (%v)", what, got, err)
	}
	wb, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("%s: marshal want: %v", what, err)
	}
	if err := json.Unmarshal(wb, &w); err != nil {
		t.Fatalf("%s: unmarshal want: %v", what, err)
	}
	if !jsonEqual(g, w) {
		t.Errorf("%s mismatch\n got: %s\nwant: %s", what, compact(got), wb)
	}
}

func compact(b []byte) []byte {
	var buf bytes.Buffer
	_ = json.Compact(&buf, b)
	return buf.Bytes()
}

// jsonEqual compares decoded values with float normalization.
func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// sseEvent is one parsed SSE frame.
type sseEvent struct {
	data      string // joined data lines
	dataLines int
	comment   string
}

type sseReader struct {
	t       *testing.T
	resp    *http.Response
	ch      chan sseEvent
	scanErr error
	// rawLines carries every physical line for framing assertions.
	rawLines chan string
}

// openStream posts a streaming method and parses its SSE frames.
func (h *harness) openStream(owner, method string, params any) *sseReader {
	h.t.Helper()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": "call-1", "method": method, "params": params}
	if params == nil {
		reqBody["params"] = struct{}{}
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		h.t.Fatalf("marshal stream request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.ts.URL, bytes.NewReader(b))
	if err != nil {
		h.t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if owner != "" {
		req.Header.Set("X-Owner", owner)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("open %s stream: %v", method, err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		_ = resp.Body.Close()
		h.t.Fatalf("%s content type = %q, want text/event-stream", method, ct)
	}
	r := &sseReader{t: h.t, resp: resp, ch: make(chan sseEvent, 64), rawLines: make(chan string, 1024)}
	go r.pump()
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return r
}

func (r *sseReader) pump() {
	sc := bufio.NewScanner(r.resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var data []string
	for sc.Scan() {
		line := sc.Text()
		select {
		case r.rawLines <- line:
		default:
		}
		switch {
		case line == "":
			if len(data) > 0 {
				r.ch <- sseEvent{data: joinData(data), dataLines: len(data)}
				data = nil
			}
		case strings.HasPrefix(line, ":"):
			r.ch <- sseEvent{comment: line}
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(line, "data:"))
		}
	}
	r.scanErr = sc.Err() // nil on clean EOF; surfaced for stall diagnosis
	close(r.ch)
}

func joinData(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// next waits for the next event or EOF, failing the test on stall.
func (r *sseReader) next(timeout time.Duration) (sseEvent, bool) {
	r.t.Helper()
	select {
	case ev, ok := <-r.ch:
		return ev, ok
	case <-time.After(timeout):
		r.t.Fatalf("SSE stream stalled for %v", timeout)
		return sseEvent{}, false
	}
}

// nextResult waits for the next data event and decodes it as a
// JSON-RPC response whose result is a StreamResponse.
func (r *sseReader) nextResult(timeout time.Duration) (*env, StreamResponse) {
	r.t.Helper()
	for {
		ev, ok := r.next(timeout)
		if !ok {
			r.t.Fatalf("stream ended before a data event arrived")
		}
		if ev.comment != "" {
			continue
		}
		var e env
		if err := json.Unmarshal([]byte(ev.data), &e); err != nil {
			r.t.Fatalf("event is not a JSON-RPC response: %q (%v)", ev.data, err)
		}
		var sr StreamResponse
		if err := json.Unmarshal(e.Result, &sr); err != nil {
			r.t.Fatalf("event result is not a StreamResponse: %s (%v)", e.Result, err)
		}
		return &e, sr
	}
}

// eof asserts the stream ends within timeout.
func (r *sseReader) eof(timeout time.Duration) {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			r.t.Fatalf("stream did not close within %v", timeout)
		}
		if _, ok := r.next(remain); !ok {
			return
		}
		// drain trailing keep-alives/comments until the close
	}
}

// waitTask polls GetTask until the task reaches want or the deadline.
func (h *harness) waitTask(owner, id string, want TaskState, timeout time.Duration) *Task {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_, e, _ := h.call(owner, MethodGetTask, map[string]any{"id": id})
		if e.Error == nil {
			var task Task
			if err := json.Unmarshal(e.Result, &task); err == nil && task.ID != "" && task.Status.State == want {
				return &task
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("task %s never reached %s", id, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
