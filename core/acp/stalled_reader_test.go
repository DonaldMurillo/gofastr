package acp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// stallWriter passes the first allow frames, then blocks every write
// until release closes: a client that stopped reading its input.
type stallWriter struct {
	mu      sync.Mutex
	allow   int
	passed  chan []byte
	release chan struct{}
}

func (w *stallWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.allow > 0 {
		w.allow--
		w.mu.Unlock()
		w.passed <- append([]byte(nil), p...)
		return len(p), nil
	}
	w.mu.Unlock()
	<-w.release
	return 0, io.ErrClosedPipe
}

// A client that sends its next prompt the moment it reads a turn's
// response is never refused as busy: the turn is released before its
// response goes out, and bounding stalled responders must not move that.
func TestPromptAfterResponseNotRefused(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	sid := d.newSession("/tmp/p")
	for i := range 50 {
		id := 100 + i
		d.request(id, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "go"}}})
		if f := d.untilResponseID(id); f["error"] != nil {
			t.Fatalf("prompt %d sent right after the last response: %v", i, f["error"])
		}
	}
}

// A client that stops reading cannot make the agent park a goroutine
// per prompt. The turn cleared its busy flag before its response went
// out, and the response then waited on the stalled write, so every
// further session/prompt on the same session passed the busy check and
// left one more responder parked behind it. A turn's response now has
// to be written before the next prompt on its session is decided.
func TestStalledReaderBoundsPromptResponders(t *testing.T) {
	sess := &fakeSession{id: "s1"}
	agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) { return sess, nil }}
	srv := acp.NewServer(agent, nil)
	out := &stallWriter{allow: 2, passed: make(chan []byte, 2), release: make(chan struct{})}
	in, clientW := io.Pipe()
	served := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background(), in, out)
		close(served)
	}()
	t.Cleanup(func() {
		close(out.release)
		_ = clientW.Close()
		select {
		case <-served:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not return")
		}
	})
	write := func(id int, method string, params any) error {
		buf, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		buf = normalizeTestCWDs(buf)
		_, err := clientW.Write(append(buf, '\n'))
		return err
	}
	if err := write(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "c", "version": "0"}}); err != nil {
		t.Fatal(err)
	}
	<-out.passed
	if err := write(2, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}}); err != nil {
		t.Fatal(err)
	}
	var newResp map[string]any
	if err := json.Unmarshal(<-out.passed, &newResp); err != nil || newResp["result"] == nil {
		t.Fatalf("session/new: %v %v", newResp, err)
	}

	before := runtime.NumGoroutine()
	const prompts = 50
	go func() {
		for i := range prompts {
			if write(10+i, "session/prompt", map[string]any{"sessionId": "s1",
				"prompt": []any{map[string]any{"type": "text", "text": fmt.Sprint(i)}}}) != nil {
				return
			}
			time.Sleep(5 * time.Millisecond) // paced: each turn has cleared busy
		}
	}()
	time.Sleep(time.Duration(prompts)*5*time.Millisecond + 300*time.Millisecond)
	if grown := runtime.NumGoroutine() - before; grown > 10 {
		t.Fatalf("%d prompts to a stalled client parked %d goroutines, want a handful", prompts, grown)
	}
}
