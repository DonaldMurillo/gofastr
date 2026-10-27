package acp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// Serve returns ctx.Err() when its context ends while the input sits
// idle: the read loop checked the context only after a line arrived, so
// an agent on a quiet stdin never stopped on shutdown.
func TestServeCancelWhileIdle(t *testing.T) {
	in, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	srv := acp.NewServer(&fakeAgent{}, nil)
	go func() { done <- srv.Serve(ctx, in, io.Discard) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Serve = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve still blocked on idle input after its context was canceled")
	}
}

// A session/load of a session whose prompt turn is still running is
// refused: replacing the session's state mid-turn cleared its busy flag
// (a second prompt then ran concurrently) and dropped the turn's cancel
// handle (session/cancel no longer reached it).
func TestLoadRefusedDuringPrompt(t *testing.T) {
	started := make(chan struct{})
	sess := &fakeSession{id: "s1", promptFn: func(ctx context.Context, _ []acp.ContentBlock, _ *acp.Client) (string, error) {
		close(started)
		<-ctx.Done()
		return acp.StopCancelled, nil
	}}
	agent := &fakeLoadingAgent{
		fakeAgent: fakeAgent{newFn: func(context.Context, string) (acp.Session, error) { return sess, nil }},
		loadFn:    func(context.Context, string, string, *acp.Client) (acp.Session, error) { return sess, nil },
	}
	d := startDialog(t, agent, nil)
	sid := d.newSession("/tmp/p")
	d.request(3, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "go"}}})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt turn never started")
	}
	d.request(4, "session/load", map[string]any{"sessionId": sid, "cwd": "/tmp/p", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(4))
	if e["code"] != float64(acp.ErrInvalidRequest) {
		t.Fatalf("session/load during a prompt: code = %v, want %d", e["code"], acp.ErrInvalidRequest)
	}
	// The running turn kept its cancel handle.
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	res := respResult(t, d.untilResponseID(3))
	if res["stopReason"] != acp.StopCancelled {
		t.Errorf("stopReason = %v, want %q", res["stopReason"], acp.StopCancelled)
	}
}

// An evicted session whose turn has not returned yet still refuses a
// reload: eviction dropped its busy flag with its entry, so a reload
// seated a fresh one and a second prompt ran beside the first.
func TestLoadRefusedWhileEvictedTurnRuns(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	first := &fakeSession{id: "s1", promptFn: func(context.Context, []acp.ContentBlock, *acp.Client) (string, error) {
		close(started)
		<-release // a turn slow to honor its cancel
		return acp.StopCancelled, nil
	}}
	minted := 0
	agent := &fakeLoadingAgent{
		fakeAgent: fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
			minted++
			if minted == 1 {
				return first, nil
			}
			return &fakeSession{id: "s2"}, nil
		}},
		loadFn: func(context.Context, string, string, *acp.Client) (acp.Session, error) { return first, nil },
	}
	d := startDialog(t, agent, &acp.Options{MaxSessions: 1, SessionOverflow: acp.SessionOverflowEvictOldest})
	sid := d.newSession("/tmp/p")
	d.request(3, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "go"}}})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt turn never started")
	}
	d.request(4, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
	respResult(t, d.untilResponseID(4))
	d.request(5, "session/load", map[string]any{"sessionId": sid, "cwd": "/tmp/p", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(5))
	if e["code"] != float64(acp.ErrInvalidRequest) {
		t.Errorf("reload of an evicted session mid-turn: code = %v, want %d", e["code"], acp.ErrInvalidRequest)
	}
	close(release)
	respResult(t, d.untilResponseID(3))
	// Once the turn returned the id loads again.
	d.request(6, "session/load", map[string]any{"sessionId": sid, "cwd": "/tmp/p", "mcpServers": []any{}})
	respResult(t, d.untilResponseID(6))
}

// Serve cancels in-flight prompt turns when its input ends: a turn
// derived from the caller's context alone outlived the connection and
// pinned its goroutine until the caller's context ended, if ever.
func TestServeEOFCancelsPrompts(t *testing.T) {
	returned := make(chan struct{})
	started := make(chan struct{})
	agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
		return &fakeSession{id: "s1", promptFn: func(ctx context.Context, _ []acp.ContentBlock, _ *acp.Client) (string, error) {
			close(started)
			<-ctx.Done()
			close(returned)
			return acp.StopCancelled, nil
		}}, nil
	}}
	d := startDialog(t, agent, nil)
	sid := d.newSession("/tmp/p")
	d.request(3, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "go"}}})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt turn never started")
	}
	_ = d.w.Close()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt turn still running after Serve's input closed")
	}
}

// Frames that are not JSON-RPC 2.0 Request objects are refused with
// -32600 and a null id (JSON-RPC 2.0 §4, §5): an id that is not a
// string, number or null, and a jsonrpc member that is missing or not
// exactly "2.0".
func TestEnvelopeRefusesInvalidRequest(t *testing.T) {
	cases := map[string]string{
		"array id":        `{"jsonrpc":"2.0","id":[],"method":"initialize","params":{}}`,
		"object id":       `{"jsonrpc":"2.0","id":{},"method":"initialize","params":{}}`,
		"bool id":         `{"jsonrpc":"2.0","id":true,"method":"initialize","params":{}}`,
		"missing jsonrpc": `{"id":1,"method":"initialize","params":{}}`,
		"jsonrpc 1.0":     `{"jsonrpc":"1.0","id":1,"method":"initialize","params":{}}`,
		"notification":    `{"method":"session/cancel","params":{"sessionId":"x"}}`,
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			d := startDialog(t, &fakeAgent{}, nil)
			d.send(json.RawMessage(frame))
			f := d.frame()
			id, present := f["id"]
			if !present || id != nil {
				t.Errorf("id = %v (present=%v), want null", id, present)
			}
			e := respError(t, f)
			if e["code"] != float64(acp.ErrInvalidRequest) {
				t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidRequest)
			}
		})
	}
	// The valid id shapes still dispatch.
	for name, frame := range map[string]string{
		"string id": `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{}}`,
		"null id":   `{"jsonrpc":"2.0","id":null,"method":"initialize","params":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			d := startDialog(t, &fakeAgent{}, nil)
			d.send(json.RawMessage(frame))
			if f := d.frame(); f["error"] != nil {
				t.Errorf("valid request refused: %v", f["error"])
			}
		})
	}
}
