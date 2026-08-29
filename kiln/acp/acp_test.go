package acp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	acpcore "github.com/DonaldMurillo/gofastr/core/acp"
	"github.com/DonaldMurillo/gofastr/framework"
	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// scriptedProvider answers each Stream call with the next scripted
// turn, recording the conversation it was shown.
type scriptedProvider struct {
	mu    sync.Mutex
	turns []agent.Turn
	seen  []agent.Request
}

func (p *scriptedProvider) Stream(ctx context.Context, req agent.Request) (agent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, req)
	if len(p.turns) == 0 {
		return agent.Turn{StopReason: "end_turn"}, nil
	}
	t := p.turns[0]
	p.turns = p.turns[1:]
	return t, nil
}

func newTools(t *testing.T) *protocol.Tools {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-acp-adapter")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l)
}

// dialog is a minimal ACP client over pipes, mirroring the core/acp
// test harness.
type dialog struct {
	t       *testing.T
	writeMu sync.Mutex
	w       io.WriteCloser
	frames  chan map[string]any
	done    chan struct{}
}

func startDialog(t *testing.T, srv *acpcore.Server) *dialog {
	t.Helper()
	srvIn, clientW := io.Pipe()
	clientR, srvOut := io.Pipe()
	d := &dialog{
		t:      t,
		w:      clientW,
		frames: make(chan map[string]any, 64),
		done:   make(chan struct{}),
	}
	go func() {
		_ = srv.Serve(context.Background(), srvIn, srvOut)
		srvOut.Close()
		close(d.done)
	}()
	go func() {
		sc := bufio.NewScanner(clientR)
		for sc.Scan() {
			var f map[string]any
			if json.Unmarshal(sc.Bytes(), &f) == nil {
				d.frames <- f
			}
		}
	}()
	t.Cleanup(func() { clientW.Close() })
	return d
}

func (d *dialog) send(v any) {
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

func (d *dialog) request(id int, method string, params any) {
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	d.send(body)
}

func (d *dialog) frame() map[string]any {
	d.t.Helper()
	select {
	case f := <-d.frames:
		return f
	case <-time.After(3 * time.Second):
		d.t.Fatal("timed out waiting for a frame")
		return nil
	}
}

// untilResponse collects notifications until the response with id.
func (d *dialog) untilResponse(id int) (map[string]any, []map[string]any) {
	d.t.Helper()
	var notifs []map[string]any
	for {
		f := d.frame()
		if got, ok := f["id"].(float64); ok && int(got) == id {
			return f, notifs
		}
		notifs = append(notifs, f)
	}
}

// handshakedDialog starts a server over tools and performs
// initialize + session/new.
func handshakedDialog(t *testing.T, tools *protocol.Tools, opts ...kilnacp.Option) (*dialog, string) {
	t.Helper()
	srv := kilnacp.NewServer(tools, opts...)
	d := startDialog(t, srv)
	d.request(1, "initialize", map[string]any{"protocolVersion": 1})
	f := d.untilResponseID(1)
	if f["error"] != nil {
		t.Fatalf("initialize: %v", f["error"])
	}
	d.request(2, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	return d, d.untilResponseID(2)["result"].(map[string]any)["sessionId"].(string)
}

func (d *dialog) untilResponseID(id int) map[string]any {
	f, _ := d.untilResponse(id)
	return f
}

func promptParams(sessionID, text string) map[string]any {
	return map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}
}

// updatesOf filters notifications down to their update bodies.
func updatesOf(t *testing.T, notifs []map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, n := range notifs {
		if n["method"] != "session/update" {
			continue
		}
		out = append(out, n["params"].(map[string]any)["update"].(map[string]any))
	}
	return out
}

// Without a provider a prompt is journaled and the turn refuses with a
// visible note, never silence.
func TestPromptWithoutProviderRefusesLoudly(t *testing.T) {
	tools := newTools(t)
	d, sid := handshakedDialog(t, tools)

	d.request(3, "session/prompt", promptParams(sid, "build me a blog"))
	resp, notifs := d.untilResponse(3)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
	if got := resp["result"].(map[string]any)["stopReason"]; got != acpcore.StopRefusal {
		t.Errorf("stopReason = %v, want refusal", got)
	}
	ups := updatesOf(t, notifs)
	if len(ups) != 1 || ups[0]["sessionUpdate"] != acpcore.UpdateAgentMessageChunk {
		t.Fatalf("updates = %v, want one agent_message_chunk", ups)
	}
	if !strings.Contains(ups[0]["content"].(map[string]any)["text"].(string), "no model provider") {
		t.Errorf("refusal note should name the missing provider: %v", ups[0])
	}
	// The user message still reached the journal (visible in the panel).
	tools.Live().ReadSession(func(s *journal.Session) {
		if len(s.Chat) != 2 || s.Chat[0].Message == nil || s.Chat[0].Message.Text != "build me a blog" {
			t.Errorf("chat journal = %+v, want user + assistant note", s.Chat)
		}
	})
}

// The full happy path: one provider turn calls add_entity, the next
// ends the conversation; every tool invocation surfaces as tool_call /
// tool_call_update frames with the result attached.
func TestPromptStreamsToolCallFrames(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{{
			CallID: "c1",
			Name:   "add_entity",
			Args: map[string]any{
				"entity": map[string]any{
					"name":   "posts",
					"fields": []any{map[string]any{"name": "title", "type": "string"}},
				},
			},
		}}, StopReason: "tool_use"},
		{Text: "posts added", StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))

	d.request(3, "session/prompt", promptParams(sid, "add a posts entity"))
	resp, notifs := d.untilResponse(3)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
	if got := resp["result"].(map[string]any)["stopReason"]; got != acpcore.StopEndTurn {
		t.Errorf("stopReason = %v, want end_turn", got)
	}

	ups := updatesOf(t, notifs)
	var kinds []string
	for _, u := range ups {
		kinds = append(kinds, u["sessionUpdate"].(string))
	}
	want := []string{
		acpcore.UpdateToolCall,       // pending
		acpcore.UpdateToolCallUpdate, // in_progress
		acpcore.UpdateToolCallUpdate, // completed
		acpcore.UpdateAgentMessageChunk,
	}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("update sequence = %v, want %v", kinds, want)
	}
	first := ups[0]
	if first["toolCallId"] != "c1" || first["title"] != "add entity" || first["status"] != acpcore.ToolStatusPending {
		t.Errorf("tool_call frame = %v", first)
	}
	last := ups[2]
	if last["status"] != acpcore.ToolStatusCompleted {
		t.Errorf("final tool status = %v, want completed", last["status"])
	}
	if _, ok := tools.Live().Session().World.Entities["posts"]; !ok {
		t.Error("posts entity missing after the turn")
	}
	// The follow-up text was journaled as the assistant reply.
	if prov.seen[len(prov.seen)-1].Messages[len(prov.seen[len(prov.seen)-1].Messages)-1].Role != "tool_result" {
		t.Error("second turn should have seen the tool result")
	}
}

// A failing dispatch must mark the tool call failed, not completed,
// and feed the error back to the model.
func TestFailedToolCallMarksFrameFailed(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{{
			CallID: "c1", Name: "add_field",
			Args: map[string]any{"entity": "missing", "field": map[string]any{"name": "x", "type": "string"}},
		}}, StopReason: "tool_use"},
		{StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))

	d.request(3, "session/prompt", promptParams(sid, "add a field"))
	resp, notifs := d.untilResponse(3)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
	ups := updatesOf(t, notifs)
	if len(ups) < 3 {
		t.Fatalf("updates = %v", ups)
	}
	final := ups[2]
	if final["status"] != acpcore.ToolStatusFailed {
		t.Errorf("final status = %v, want failed", final["status"])
	}
	content := final["content"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(content, "not_found") && !strings.Contains(content, "error") {
		t.Errorf("failure content should carry the error: %q", content)
	}
}

// promptAnsweringPermission sends session/prompt, answers any
// session/request_permission the server issues with the option of the
// given kind, and returns the prompt response plus the notifications
// seen along the way. Reading is sequential: one goroutine owns the
// frame stream, so the permission request cannot be consumed by the
// response waiter.
func (d *dialog) promptAnsweringPermission(id int, params map[string]any, answerKind string) (map[string]any, []map[string]any) {
	d.t.Helper()
	d.request(id, "session/prompt", params)
	var notifs []map[string]any
	for {
		f := d.frame()
		if f["method"] == "session/request_permission" {
			var optID string
			for _, o := range f["params"].(map[string]any)["options"].([]any) {
				if m := o.(map[string]any); m["kind"] == answerKind {
					optID = m["optionId"].(string)
				}
			}
			if optID == "" {
				d.t.Fatalf("no %s option in %v", answerKind, f["params"])
			}
			d.send(map[string]any{
				"jsonrpc": "2.0", "id": int(f["id"].(float64)),
				"result": map[string]any{"outcome": map[string]any{"outcome": acpcore.OutcomeSelected, "optionId": optID}},
			})
			continue
		}
		if got, ok := f["id"].(float64); ok && int(got) == id {
			return f, notifs
		}
		notifs = append(notifs, f)
	}
}

// approve_plan never dispatches without the user's explicit allow at
// the client; a rejection is fed back to the model as needs_plan.
func TestApprovePlanGatedOnUserPermission(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{{CallID: "c1", Name: "approve_plan", Args: map[string]any{"plan_id": "p1"}}}, StopReason: "tool_use"},
		{StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))

	resp, notifs := d.promptAnsweringPermission(3, promptParams(sid, "approve it"), acpcore.PermissionRejectOnce)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
	ups := updatesOf(t, notifs)
	if len(ups) != 2 {
		t.Fatalf("updates = %v, want pending tool_call + failed update", ups)
	}
	if ups[1]["status"] != acpcore.ToolStatusFailed {
		t.Errorf("rejected approve_plan status = %v, want failed", ups[1]["status"])
	}
	// The model sees the rejection as a tool result.
	last := prov.seen[len(prov.seen)-1]
	tr := last.Messages[len(last.Messages)-1]
	if tr.Role != "tool_result" || tr.Result == nil || tr.Result.Kind != "needs_plan" {
		t.Errorf("model feedback = %+v, want needs_plan tool_result", tr)
	}
}

// The allow path really dispatches approve_plan after the user allows.
func TestApprovePlanAllowedByUserDispatches(t *testing.T) {
	tools := newTools(t)
	prov := &scriptedProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{{CallID: "c1", Name: "approve_plan", Args: map[string]any{"plan_id": "p1"}}}, StopReason: "tool_use"},
		{StopReason: "end_turn"},
	}}
	d, sid := handshakedDialog(t, tools, kilnacp.WithProvider(prov))

	resp, notifs := d.promptAnsweringPermission(3, promptParams(sid, "approve it"), acpcore.PermissionAllowOnce)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
	ups := updatesOf(t, notifs)
	if len(ups) < 3 {
		t.Fatalf("updates = %v, want tool_call + updates", ups)
	}
	// The dispatch ran only after the allow: the in_progress update
	// proves the turn continued past the permission round trip (the
	// plan itself is unknown here, so the result update reports the
	// dispatch failure rather than a silent skip).
	if ups[1]["status"] != acpcore.ToolStatusInProgress {
		t.Errorf("allowed approve_plan never ran: %v", ups)
	}
	if ups[2]["status"] != acpcore.ToolStatusFailed {
		t.Errorf("unknown-plan approve result should surface as failed, got %v", ups[2])
	}
}

// session/load replays the journaled conversation before responding.
func TestLoadSessionReplaysKilnChat(t *testing.T) {
	tools := newTools(t)
	d, sid := handshakedDialog(t, tools)

	d.request(3, "session/prompt", promptParams(sid, "hello"))
	d.untilResponseID(3) // refusal path journals user + assistant note

	d.request(4, "session/load", map[string]any{"sessionId": sid, "cwd": t.TempDir(), "mcpServers": []any{}})
	resp, notifs := d.untilResponse(4)
	if resp["error"] != nil {
		t.Fatalf("session/load errored: %v", resp["error"])
	}
	ups := updatesOf(t, notifs)
	if len(ups) != 2 {
		t.Fatalf("replay updates = %v, want user + assistant chunks", ups)
	}
	if ups[0]["sessionUpdate"] != acpcore.UpdateUserMessageChunk || ups[1]["sessionUpdate"] != acpcore.UpdateAgentMessageChunk {
		t.Errorf("replay order wrong: %v", ups)
	}
}

func TestLoadUnknownSessionIsResourceNotFound(t *testing.T) {
	tools := newTools(t)
	d, _ := handshakedDialog(t, tools)
	d.request(4, "session/load", map[string]any{"sessionId": "from-a-previous-run", "cwd": t.TempDir(), "mcpServers": []any{}})
	resp := d.untilResponseID(4)
	e, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error, got %v", resp)
	}
	if e["code"] != float64(acpcore.ErrResourceNotFound) {
		t.Errorf("code = %v, want %d", e["code"], acpcore.ErrResourceNotFound)
	}
}
