package kilntest

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
)

// ScriptedProvider answers each Stream call with the next scripted
// turn, recording the requests it was shown.
type ScriptedProvider struct {
	mu    sync.Mutex
	Turns []agent.Turn
	Seen  []agent.Request
}

func (p *ScriptedProvider) Stream(ctx context.Context, req agent.Request) (agent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Seen = append(p.Seen, req)
	if len(p.Turns) == 0 {
		return agent.Turn{StopReason: "end_turn"}, nil
	}
	t := p.Turns[0]
	p.Turns = p.Turns[1:]
	return t, nil
}

// PipeDialog is a minimal ACP JSON-RPC client over a pipe pair: Send
// writes newline-delimited request frames, and the reader goroutine
// started by StartPipeDialog decodes the server's frames into Frames.
type PipeDialog struct {
	t       *testing.T
	writeMu sync.Mutex
	w       io.WriteCloser
	Frames  chan map[string]any
}

// StartPipeDialog wires the client end of a pipe pair: w carries the
// client's frames to the server, r carries the server's frames back.
func StartPipeDialog(t *testing.T, w io.WriteCloser, r io.Reader) *PipeDialog {
	t.Helper()
	d := &PipeDialog{t: t, w: w, Frames: make(chan map[string]any, 64)}
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			var f map[string]any
			if jsonexact.Unmarshal(sc.Bytes(), &f) == nil {
				d.Frames <- f
			}
		}
		if err := sc.Err(); err != nil {
			t.Errorf("client frame reader: %v", err) // else: silent timeout
		}
	}()
	t.Cleanup(func() { w.Close() })
	return d
}

// Send writes one JSON frame to the server.
func (d *PipeDialog) Send(v any) {
	d.t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		d.t.Fatal(err)
	}
	d.writeMu.Lock()
	_, err = d.w.Write(append(buf, '\n'))
	d.writeMu.Unlock()
	if err != nil {
		d.t.Fatalf("write: %v", err)
	}
}

// Request sends a jsonrpc request frame.
func (d *PipeDialog) Request(id int, method string, params any) {
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	d.Send(body)
}

// Frame reads the next server frame, failing the test on timeout.
func (d *PipeDialog) Frame() map[string]any {
	d.t.Helper()
	select {
	case f := <-d.Frames:
		return f
	case <-time.After(5 * time.Second):
		d.t.Fatal("timed out waiting for a server frame")
		return nil
	}
}
