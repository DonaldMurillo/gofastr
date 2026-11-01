package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// hangingProvider streams nothing until its context ends, so a turn on
// it runs until somebody cancels it.
type hangingProvider struct{ fakeProvider }

func (hangingProvider) Chat(ctx context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// pipeServer runs a stdio Server over pipes and returns a line writer,
// a response reader and the session's bus.
func pipeServer(t *testing.T) (send func(map[string]any), recv func() map[string]any, sess ids.SessionID, bus *engine.Bus) {
	t.Helper()
	mux := multiplex.New()
	cat := resources.NewCatalog()
	sess = ids.NewSessionID()
	bus = engine.NewBus(sess)
	reg := tool.NewRegistry()
	cat.Tools = reg
	cat.Skills = func() []skillmd.Tier1 { return nil }
	eng := engine.NewEngine(sess, bus, hangingProvider{}, "fake", engine.NewDispatcher(bus, reg))
	mux.RegisterEngine(eng)
	cat.RegisterEngine(eng)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := New(mux, cat).WithIO(inR, outW)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Serve(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = outR.Close()
		<-done
		bus.Close()
	})

	lines := make(chan map[string]any, 16)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				lines <- m
			}
		}
		// The pipe closes at cleanup, so a scan error only means the
		// server side is gone; recv then times out as "no response".
		_ = sc.Err()
	}()
	send = func(m map[string]any) {
		b, _ := json.Marshal(m)
		go func() { _, _ = inW.Write(append(b, '\n')) }()
	}
	recv = func() map[string]any {
		select {
		case m := <-lines:
			return m
		case <-time.After(3 * time.Second):
			return nil
		}
	}
	return send, recv, sess, bus
}

func callTool(id int, name string, args map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	}
}

// cancel_turn sent while a wait=turn call is running is read and
// answered, and it ends the turn the first call waits on.
func TestCancelTurnReachesRunningTurn(t *testing.T) {
	send, recv, sess, _ := pipeServer(t)
	send(callTool(1, "harness.run_agent_with_shell_access", map[string]any{
		"sessionId": string(sess), "prompt": "hi", "wait": "turn",
	}))
	time.Sleep(100 * time.Millisecond) // let the turn start
	send(callTool(2, "harness.cancel_turn", map[string]any{"sessionId": string(sess)}))
	got := map[float64]bool{}
	for range 2 {
		m := recv()
		if m == nil {
			t.Fatalf("no response; got ids %v", got)
		}
		id, _ := m["id"].(float64)
		got[id] = true
	}
	if !got[1] || !got[2] {
		t.Fatalf("responses for ids %v, want 1 and 2", got)
	}
}

// notifications/cancelled for a wait=turn call stops waiting on it and
// cancels the turn it started; no response is sent for it.
func TestCancelledNotificationEndsTurn(t *testing.T) {
	send, recv, sess, bus := pipeServer(t)
	events := bus.Subscribe(t.Context())
	send(callTool(1, "harness.run_agent_with_shell_access", map[string]any{
		"sessionId": string(sess), "prompt": "hi", "wait": "turn",
	}))
	time.Sleep(100 * time.Millisecond)
	send(map[string]any{
		"jsonrpc": "2.0", "method": "notifications/cancelled",
		"params": map[string]any{"requestId": 1, "reason": "user hit esc"},
	})
	deadline := time.After(3 * time.Second)
	for ended := false; !ended; {
		select {
		case env := <-events:
			ev, _ := control.DecodeEvent(env)
			if _, ok := ev.(control.TurnEnded); ok {
				ended = true
			}
		case <-deadline:
			t.Fatal("turn still running after notifications/cancelled")
		}
	}
	// The server keeps serving, and the cancelled call got no answer.
	send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/list"})
	m := recv()
	if m == nil {
		t.Fatal("server stopped answering")
	}
	if id, _ := m["id"].(float64); id != 3 {
		t.Fatalf("got response %v, want only id 3", m)
	}
}

// A second request reusing a running request's id is refused, so a
// cancel for that id can only ever name one call.
func TestDuplicateInFlightIDRefused(t *testing.T) {
	send, recv, sess, _ := pipeServer(t)
	send(callTool(1, "harness.run_agent_with_shell_access", map[string]any{
		"sessionId": string(sess), "prompt": "hi", "wait": "turn",
	}))
	time.Sleep(100 * time.Millisecond)
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	m := recv()
	if m == nil {
		t.Fatal("no response")
	}
	if m["error"] == nil {
		t.Fatalf("reused id answered: %v", m)
	}
}
