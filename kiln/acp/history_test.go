package acp_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	acpcore "github.com/DonaldMurillo/gofastr/core/acp"
	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// convo renders a provider conversation one message per line, so a
// test can compare what the model was shown.
func convo(msgs []agent.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			var ids []string
			for _, c := range m.ToolCalls {
				ids = append(ids, c.CallID)
			}
			fmt.Fprintf(&b, "assistant:%s%v\n", m.Text, ids)
		case "tool_result":
			fmt.Fprintf(&b, "tool_result:%s ok=%v\n", m.ToolUseID, m.Result != nil && m.Result.OK)
		default:
			fmt.Fprintf(&b, "%s:%s\n", m.Role, m.Text)
		}
	}
	return b.String()
}

// A second prompt in a session shows the model the first one: its
// text, the tool calls it made and their results (2026-10-27 K7).
func TestPromptSeesEarlierPrompts(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{postsCall("c1")}, StopReason: "tool_use"},
		{Text: "posts added", StopReason: "end_turn"},
		{Text: "second reply", StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))
	d.request(3, "session/prompt", promptParams(sid, "add posts"))
	d.untilResponseID(3)
	d.request(4, "session/prompt", promptParams(sid, "now what"))
	d.untilResponseID(4)

	want := "user:add posts\n" +
		"assistant:[c1]\n" +
		"tool_result:c1 ok=true\n" +
		"assistant:posts added[]\n" +
		"user:now what\n"
	if got := convo(prov.seen[2].Messages); got != want {
		t.Errorf("second prompt showed the model:\n%s\nwant:\n%s", got, want)
	}
}

// Sessions keep their own conversations.
func TestSessionHistoriesAreSeparate(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{Text: "one", StopReason: "end_turn"},
		{Text: "two", StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))
	d.request(3, "session/prompt", promptParams(sid, "first"))
	d.untilResponseID(3)
	d.request(4, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	other := d.untilResponseID(4)["result"].(map[string]any)["sessionId"].(string)
	d.request(5, "session/prompt", promptParams(other, "fresh"))
	d.untilResponseID(5)
	if got := convo(prov.seen[1].Messages); got != "user:fresh\n" {
		t.Errorf("a new session showed the model:\n%s", got)
	}
}

// A prompt cancelled between tool calls leaves every call of that
// assistant turn answered: the one that ran with its result, the one
// that did not with a failure saying so. Anthropic's and OpenAI's APIs
// reject a request holding a tool call with no result, which would
// break every later prompt in the session.
func TestCancelledCallIsAnsweredNext(t *testing.T) {
	tools := newTools(t)
	held, release := make(chan struct{}), make(chan struct{})
	var (
		mu   sync.Mutex
		seen []agent.Request
	)
	prov := &funcProvider{fn: func(_ context.Context, req agent.Request) (agent.Turn, error) {
		mu.Lock()
		seen = append(seen, req)
		n := len(seen)
		mu.Unlock()
		if n > 1 {
			return agent.Turn{StopReason: "end_turn"}, nil
		}
		go tools.Live().ReadSession(func(*journal.Session) {
			close(held)
			<-release
		})
		<-held
		return agent.Turn{ToolCalls: []agent.ToolCall{
			{CallID: "c1", Name: "chat", Args: map[string]any{"role": "assistant", "text": "first"}},
			postsCall("c2"),
		}, StopReason: "tool_use"}, nil
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))
	d.request(3, "session/prompt", promptParams(sid, "two calls"))
	<-held
	for {
		f := d.frame()
		if f["method"] != "session/update" {
			continue
		}
		u := f["params"].(map[string]any)["update"].(map[string]any)
		if u["toolCallId"] == "c1" && u["status"] == "in_progress" {
			break
		}
	}
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	d.request(4, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	d.untilResponseID(4)
	close(release)
	d.untilResponseID(3)

	d.request(5, "session/prompt", promptParams(sid, "again"))
	d.untilResponseID(5)
	want := "user:two calls\n" +
		"assistant:[c1 c2]\n" +
		"tool_result:c1 ok=true\n" +
		"tool_result:c2 ok=false\n" +
		"user:again\n"
	mu.Lock()
	defer mu.Unlock()
	if got := convo(seen[1].Messages); got != want {
		t.Fatalf("the prompt after a cancel showed the model:\n%s\nwant:\n%s", got, want)
	}
	if r := seen[1].Messages[3].Result; r == nil || r.Kind != "cancelled" {
		t.Errorf("the call that never ran answered %+v, want kind cancelled", r)
	}
}

// A turn cancelled before its tool calls ran keeps the prompt and
// drops the turn: none of it happened.
func TestCancelBeforeToolsKeepsPrompt(t *testing.T) {
	tools := newTools(t)
	started := make(chan struct{})
	var (
		mu   sync.Mutex
		seen []agent.Request
	)
	prov := &funcProvider{fn: func(ctx context.Context, req agent.Request) (agent.Turn, error) {
		mu.Lock()
		seen = append(seen, req)
		n := len(seen)
		mu.Unlock()
		if n > 1 {
			return agent.Turn{StopReason: "end_turn"}, nil
		}
		close(started)
		<-ctx.Done()
		return agent.Turn{Text: "done anyway", ToolCalls: []agent.ToolCall{postsCall("c1")}, StopReason: "tool_use"}, nil
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))
	d.request(3, "session/prompt", promptParams(sid, "add posts"))
	<-started
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	d.untilResponseID(3)
	d.request(4, "session/prompt", promptParams(sid, "next"))
	d.untilResponseID(4)
	mu.Lock()
	defer mu.Unlock()
	if got := convo(seen[1].Messages); got != "user:add posts\nuser:next\n" {
		t.Errorf("the prompt after a cancel showed the model:\n%s", got)
	}
}

// Two connections that load one session take its turns one at a time:
// the second prompt reaches the provider after the first prompt's
// exchange is in the history, never beside it.
func TestSessionTurnsSerializeAcrossConnections(t *testing.T) {
	tools := newTools(t)
	first, release := make(chan struct{}), make(chan struct{})
	second := make(chan agent.Request, 1)
	var calls int
	var mu sync.Mutex
	prov := &funcProvider{fn: func(_ context.Context, req agent.Request) (agent.Turn, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(first)
			<-release
			return agent.Turn{Text: "r1", StopReason: "end_turn"}, nil
		}
		second <- req
		return agent.Turn{Text: "r2", StopReason: "end_turn"}, nil
	}}
	d1, d2, sid := sharedSession(t, kilnacp.NewServer(tools, kilnacp.WithProvider(prov)))
	d1.request(3, "session/prompt", promptParams(sid, "one"))
	<-first
	d2.request(3, "session/prompt", promptParams(sid, "two"))
	// Give a second prompt that does not wait its turn time to reach
	// the provider while the first is still running.
	select {
	case <-second:
		t.Fatal("the second prompt reached the provider while the first was running")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	d1.untilResponseID(3)
	d2.untilResponseID(3)
	req := <-second
	if got, want := convo(req.Messages), "user:one\nassistant:r1[]\nuser:two\n"; got != want {
		t.Errorf("the second connection's prompt showed the model:\n%s\nwant:\n%s", got, want)
	}
}

// sharedSession opens two connections to srv: the first creates a
// session and the second loads it.
func sharedSession(t *testing.T, srv *acpcore.Server) (*dialog, *dialog, string) {
	t.Helper()
	d1 := startDialog(t, srv)
	d1.request(1, "initialize", map[string]any{"protocolVersion": 1})
	d1.untilResponseID(1)
	d1.request(2, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	sid := d1.untilResponseID(2)["result"].(map[string]any)["sessionId"].(string)

	d2 := startDialog(t, srv)
	d2.request(1, "initialize", map[string]any{"protocolVersion": 1})
	d2.untilResponseID(1)
	d2.request(2, "session/load", map[string]any{"sessionId": sid, "cwd": t.TempDir(), "mcpServers": []any{}})
	if f := d2.untilResponseID(2); f["error"] != nil {
		t.Fatalf("session/load: %v", f["error"])
	}
	return d1, d2, sid
}

// A prompt cancelled while it waits for the session's turn answers
// cancelled at once and leaves nothing behind.
func TestCancelWhileWaitingForTurn(t *testing.T) {
	tools := newTools(t)
	first, release := make(chan struct{}), make(chan struct{})
	var calls int
	var mu sync.Mutex
	prov := &funcProvider{fn: func(context.Context, agent.Request) (agent.Turn, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(first)
			<-release
		}
		return agent.Turn{Text: "r", StopReason: "end_turn"}, nil
	}}
	d1, d2, sid := sharedSession(t, kilnacp.NewServer(tools, kilnacp.WithProvider(prov)))
	defer close(release)
	d1.request(3, "session/prompt", promptParams(sid, "one"))
	<-first
	d2.request(3, "session/prompt", promptParams(sid, "waiting"))
	d2.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	resp := d2.untilResponseID(3)
	if got := resp["result"].(map[string]any)["stopReason"]; got != acpcore.StopCancelled {
		t.Errorf("stopReason = %v, want cancelled", got)
	}
	tools.Live().ReadSession(func(s *journal.Session) {
		for _, ev := range s.Chat {
			if ev.Message != nil && ev.Message.Text == "waiting" {
				t.Error("the cancelled waiting prompt was journaled")
			}
		}
	})
}
