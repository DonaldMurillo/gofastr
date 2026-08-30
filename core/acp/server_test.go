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

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// --- scripted agent ------------------------------------------------------

// fakeSession is a scripted ACP session: promptFn runs the turn.
type fakeSession struct {
	id       string
	promptFn func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error)
}

func (s *fakeSession) ID() string { return s.id }

func (s *fakeSession) Prompt(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
	if s.promptFn == nil {
		return acp.StopEndTurn, nil
	}
	return s.promptFn(ctx, prompt, out)
}

// fakeAgent mints scripted sessions. It does NOT implement the
// SessionLoader interface; use fakeLoadingAgent where session/load
// must be available.
type fakeAgent struct {
	newFn func(ctx context.Context, cwd string) (acp.Session, error)
}

func (a *fakeAgent) Info() acp.Implementation {
	return acp.Implementation{Name: "fake", Title: "Fake Agent", Version: "1.2.3"}
}

func (a *fakeAgent) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	if a.newFn != nil {
		return a.newFn(ctx, cwd)
	}
	return &fakeSession{id: "sess_1"}, nil
}

// fakeLoadingAgent wraps fakeAgent with SessionLoader support.
type fakeLoadingAgent struct {
	fakeAgent
	loadFn func(ctx context.Context, id, cwd string, out *acp.Client) (acp.Session, error)
}

func (a *fakeLoadingAgent) LoadSession(ctx context.Context, id, cwd string, out *acp.Client) (acp.Session, error) {
	if a.loadFn != nil {
		return a.loadFn(ctx, id, cwd, out)
	}
	return nil, acp.ErrSessionNotFound
}

// --- dialog harness ------------------------------------------------------

// dialog drives one Serve connection over pipes: the test is the ACP
// client, the server under test is the agent.
type dialog struct {
	t   *testing.T
	srv *acp.Server

	writeMu sync.Mutex
	w       io.WriteCloser

	frames chan map[string]any
	done   chan struct{}
}

func startDialog(t *testing.T, agent acp.Agent, opts *acp.Options) *dialog {
	t.Helper()
	srv := acp.NewServer(agent, opts)
	srvIn, clientW := io.Pipe()  // client writes -> server reads
	clientR, srvOut := io.Pipe() // server writes -> client reads
	d := &dialog{
		t:      t,
		srv:    srv,
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
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var f map[string]any
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				continue
			}
			d.frames <- f
		}
		if err := sc.Err(); err != nil {
			t.Errorf("client frame reader: %v", err) // else: silent timeout
		}
		clientR.Close()
	}()
	t.Cleanup(func() {
		clientW.Close()
		select {
		case <-d.done:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return after input closed")
		}
	})
	return d
}

// send writes one JSON-RPC frame from the client.
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
		d.t.Fatalf("write frame: %v", err)
	}
}

func (d *dialog) request(id int, method string, params any) {
	d.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	d.send(body)
}

// frame reads the next frame from the server or fails the test.
func (d *dialog) frame() map[string]any {
	d.t.Helper()
	select {
	case f := <-d.frames:
		return f
	case <-time.After(3 * time.Second):
		d.t.Fatal("timed out waiting for a server frame")
		return nil
	}
}

// untilResponse reads frames until the response with the given id,
// returning it plus every notification seen first.
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

func respError(t *testing.T, f map[string]any) map[string]any {
	t.Helper()
	e, ok := f["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error response, got %v", f)
	}
	return e
}

func respResult(t *testing.T, f map[string]any) map[string]any {
	t.Helper()
	r, ok := f["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a result object, got %v", f)
	}
	return r
}

// initialize performs the handshake and returns the result.
func (d *dialog) initialize() map[string]any {
	d.t.Helper()
	d.request(1, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
		"clientInfo": map[string]any{"name": "test-client", "version": "0.0.0"},
	})
	f := d.untilResponseID(1)
	return respResult(d.t, f)
}

func (d *dialog) untilResponseID(id int) map[string]any {
	f, _ := d.untilResponse(id)
	return f
}

// newSession performs initialize + session/new and returns the ID.
func (d *dialog) newSession(cwd string) string {
	d.t.Helper()
	d.initialize()
	d.request(2, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	return respResult(d.t, d.untilResponseID(2))["sessionId"].(string)
}

// --- initialize ----------------------------------------------------------

// A client that offers fs and terminal support must learn at
// initialize that this agent uses none of it: the capability block
// carries explicit false values, never silence.
func TestInitializeDeclaresAbsencesExplicitly(t *testing.T) {
	d := startDialog(t, &fakeLoadingAgent{}, nil)
	res := d.initialize()

	if res["protocolVersion"] != float64(1) {
		t.Errorf("protocolVersion = %v, want 1", res["protocolVersion"])
	}
	caps, ok := res["agentCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("agentCapabilities = %v", res["agentCapabilities"])
	}
	if caps["loadSession"] != true {
		t.Errorf("loadSession = %v, want true for a loader agent", caps["loadSession"])
	}
	pc, ok := caps["promptCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("promptCapabilities = %v", caps["promptCapabilities"])
	}
	for _, k := range []string{"image", "audio", "embeddedContext"} {
		if v, present := pc[k]; !present || v != false {
			t.Errorf("promptCapabilities.%s = %v (present=%v), want explicit false", k, v, present)
		}
	}
	mc, ok := caps["mcpCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("mcpCapabilities = %v", caps["mcpCapabilities"])
	}
	for _, k := range []string{"http", "sse"} {
		if v, present := mc[k]; !present || v != false {
			t.Errorf("mcpCapabilities.%s = %v (present=%v), want explicit false", k, v, present)
		}
	}
	// fs/terminal are client capabilities; the agent response must not
	// claim any of them back.
	for _, k := range []string{"fs", "terminal", "readTextFile", "writeTextFile"} {
		if _, present := caps[k]; present {
			t.Errorf("agentCapabilities must not carry %q (client-side capability)", k)
		}
	}
	if methods, ok := res["authMethods"].([]any); !ok || len(methods) != 0 {
		t.Errorf("authMethods = %v, want empty array", res["authMethods"])
	}
	info, ok := res["agentInfo"].(map[string]any)
	if !ok || info["name"] != "fake" {
		t.Errorf("agentInfo = %v, want the agent's Info()", res["agentInfo"])
	}
}

func TestInitializeWithoutLoaderAdvertisesNoLoad(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	res := d.initialize()
	caps := res["agentCapabilities"].(map[string]any)
	if caps["loadSession"] != false {
		t.Errorf("loadSession = %v, want explicit false", caps["loadSession"])
	}
}

// --- authenticate --------------------------------------------------------

func TestAuthenticateRejectsUnadvertisedMethod(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(3, "authenticate", map[string]any{"methodId": "nope"})
	e := respError(t, d.untilResponseID(3))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
	if !strings.Contains(e["message"].(string), "nope") {
		t.Errorf("message should name the rejected methodId: %v", e["message"])
	}
}

func TestAuthenticateRunsAdvertisedMethod(t *testing.T) {
	called := false
	opts := &acp.Options{
		AuthMethods: []acp.AuthMethod{{ID: "agent-login", Name: "Agent login"}},
		Authenticate: func(ctx context.Context, methodID string) error {
			called = true
			if methodID != "agent-login" {
				t.Errorf("methodID = %q", methodID)
			}
			return nil
		},
	}
	d := startDialog(t, &fakeAgent{}, opts)
	d.initialize()
	d.request(3, "authenticate", map[string]any{"methodId": "agent-login"})
	f := d.untilResponseID(3)
	if f["error"] != nil {
		t.Fatalf("authenticate errored: %v", f["error"])
	}
	if !called {
		t.Error("Authenticate hook was not called")
	}
}

func TestAuthenticateFailureUsesAuthRequiredCode(t *testing.T) {
	opts := &acp.Options{
		AuthMethods: []acp.AuthMethod{{ID: "m", Name: "m"}},
		Authenticate: func(ctx context.Context, methodID string) error {
			return io.ErrClosedPipe
		},
	}
	d := startDialog(t, &fakeAgent{}, opts)
	d.initialize()
	d.request(3, "authenticate", map[string]any{"methodId": "m"})
	e := respError(t, d.untilResponseID(3))
	if e["code"] != float64(acp.ErrAuthRequired) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrAuthRequired)
	}
}

// --- session/new ---------------------------------------------------------

func TestSessionNewReturnsSessionID(t *testing.T) {
	d := startDialog(t, &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
		if !strings.HasPrefix(cwd, "/") {
			t.Errorf("cwd = %q, want absolute", cwd)
		}
		return &fakeSession{id: "sess_xyz"}, nil
	}}, nil)
	id := d.newSession("/tmp/proj")
	if id != "sess_xyz" {
		t.Errorf("sessionId = %q, want sess_xyz", id)
	}
}

func TestSessionNewRequiresInitializeFirst(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.request(1, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(1))
	if e["code"] != float64(acp.ErrInvalidRequest) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidRequest)
	}
}

// The agent advertises no mcpCapabilities, so MCP servers passed to
// session/new must be refused, not accepted and dropped.
func TestSessionNewRejectsMCPServers(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(2, "session/new", map[string]any{
		"cwd":        "/tmp/p",
		"mcpServers": []any{map[string]any{"name": "filesystem", "command": "/bin/mcp"}},
	})
	e := respError(t, d.untilResponseID(2))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
	if !strings.Contains(e["message"].(string), "MCP") {
		t.Errorf("message should name MCP servers: %v", e["message"])
	}
}

func TestSessionNewRejectsRelativeCwd(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(2, "session/new", map[string]any{"cwd": "relative/dir", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(2))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
}

func TestSessionNewRejectsAdditionalDirectories(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(2, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}, "additionalDirectories": []string{"/tmp/other"}})
	e := respError(t, d.untilResponseID(2))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
}

// --- session/load --------------------------------------------------------

func TestSessionLoadReplaysHistoryBeforeResponding(t *testing.T) {
	agent := &fakeLoadingAgent{}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")

	agent.loadFn = func(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
		if err := out.Update(acp.UserMessageChunk("m1", "what is 1+1?")); err != nil {
			return nil, err
		}
		if err := out.Update(acp.AgentMessageChunk("m2", "2")); err != nil {
			return nil, err
		}
		return &fakeSession{id: sid}, nil
	}
	d.request(4, "session/load", map[string]any{"sessionId": id, "cwd": "/tmp/p", "mcpServers": []any{}})
	resp, notifs := d.untilResponse(4)
	if resp["error"] != nil {
		t.Fatalf("session/load errored: %v", resp["error"])
	}
	if len(notifs) != 2 {
		t.Fatalf("got %d replay notifications, want 2: %v", len(notifs), notifs)
	}
	first := notifs[0]["params"].(map[string]any)
	if first["sessionId"] != id {
		t.Errorf("replay sessionId = %v, want %q", first["sessionId"], id)
	}
	upd := first["update"].(map[string]any)
	if upd["sessionUpdate"] != acp.UpdateUserMessageChunk {
		t.Errorf("first replay update = %v, want user_message_chunk", upd)
	}
}

func TestSessionLoadUnknownIDIsResourceNotFound(t *testing.T) {
	d := startDialog(t, &fakeLoadingAgent{}, nil)
	d.initialize()
	d.request(2, "session/load", map[string]any{"sessionId": "nope", "cwd": "/tmp/p", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(2))
	if e["code"] != float64(acp.ErrResourceNotFound) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrResourceNotFound)
	}
}

func TestSessionLoadWithoutCapabilityIsMethodNotFound(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil) // no loader
	d.initialize()
	d.request(2, "session/load", map[string]any{"sessionId": "x", "cwd": "/tmp/p", "mcpServers": []any{}})
	e := respError(t, d.untilResponseID(2))
	if e["code"] != float64(acp.ErrMethodNotFound) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrMethodNotFound)
	}
}

// --- session/prompt ------------------------------------------------------

func TestSessionPromptStreamsUpdatesThenStops(t *testing.T) {
	agent := &fakeAgent{}
	agent.newFn = func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "s1", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			if txt := acp.PromptText(prompt); txt != "hello there" {
				t.Errorf("PromptText = %q", txt)
			}
			_ = out.Update(acp.AgentMessageChunk("m1", "working"))
			_ = out.Update(acp.NewToolCall(acp.ToolCall{ToolCallID: "c1", Title: "add_entity", Kind: acp.ToolKindEdit, Status: acp.ToolStatusPending}))
			_ = out.Update(acp.ToolCallUpdateFrame(acp.ToolCallUpdate{ToolCallID: "c1", Status: new(acp.ToolStatusInProgress)}))
			_ = out.Update(acp.ToolCallUpdateFrame(acp.ToolCallUpdate{
				ToolCallID: "c1", Status: new(acp.ToolStatusCompleted),
				Content: []acp.ToolCallContent{acp.TextToolContent("ok")},
			}))
			return acp.StopEndTurn, nil
		}}, nil
	}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{
		"sessionId": id,
		"prompt":    []any{map[string]any{"type": "text", "text": "hello there"}},
	})
	resp, notifs := d.untilResponse(5)
	if resp["error"] != nil {
		t.Fatalf("session/prompt errored: %v", resp["error"])
	}
	if got := respResult(t, resp)["stopReason"]; got != acp.StopEndTurn {
		t.Errorf("stopReason = %v, want end_turn", got)
	}
	if len(notifs) != 4 {
		t.Fatalf("got %d updates, want 4 (chunk, tool_call, 2 updates)", len(notifs))
	}
	// The response must land after every update the turn produced.
	lastUpd := notifs[3]["params"].(map[string]any)["update"].(map[string]any)
	if lastUpd["status"] != acp.ToolStatusCompleted {
		t.Errorf("last update status = %v, want completed", lastUpd["status"])
	}
}

// initialize declares image/audio/embeddedContext false; a client
// that sends an image block anyway gets a rejection that says so.
func TestSessionPromptRejectsImageBlocks(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{
		"sessionId": id,
		"prompt": []any{
			map[string]any{"type": "text", "text": "look"},
			map[string]any{"type": "image", "data": "…", "mimeType": "image/png"},
		},
	})
	e := respError(t, d.untilResponseID(5))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
	if !strings.Contains(e["message"].(string), "image") {
		t.Errorf("message should name the rejected block type: %v", e["message"])
	}
}

func TestSessionPromptEmptyPromptIsRejected(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{"sessionId": id, "prompt": []any{}})
	e := respError(t, d.untilResponseID(5))
	if e["code"] != float64(acp.ErrInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidParams)
	}
}

func TestSessionPromptUnknownSessionIsNotFound(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(5, "session/prompt", map[string]any{
		"sessionId": "ghost",
		"prompt":    []any{map[string]any{"type": "text", "text": "hi"}},
	})
	e := respError(t, d.untilResponseID(5))
	if e["code"] != float64(acp.ErrResourceNotFound) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrResourceNotFound)
	}
}

func TestSessionPromptSecondTurnWhileBusyIsRejected(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "s1", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			close(started)
			<-release
			return acp.StopEndTurn, nil
		}}, nil
	}}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	prompt := map[string]any{"sessionId": id, "prompt": []any{map[string]any{"type": "text", "text": "one"}}}
	d.request(5, "session/prompt", prompt)
	<-started
	d.request(6, "session/prompt", prompt)
	e := respError(t, d.untilResponseID(6))
	if e["code"] != float64(acp.ErrInvalidRequest) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrInvalidRequest)
	}
	close(release)
	d.untilResponseID(5)
}

// --- session/cancel ------------------------------------------------------

func TestSessionCancelReturnsCancelledStopReason(t *testing.T) {
	started := make(chan struct{})
	agent := &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "s1", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			close(started)
			<-ctx.Done()
			// Even an implementation error must surface as cancelled.
			return "", io.ErrClosedPipe
		}}, nil
	}}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{
		"sessionId": id,
		"prompt":    []any{map[string]any{"type": "text", "text": "slow"}},
	})
	<-started
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": id}})
	resp := d.untilResponseID(5)
	if got := respResult(t, resp)["stopReason"]; got != acp.StopCancelled {
		t.Errorf("stopReason = %v, want cancelled", got)
	}
}

// session/cancel for an unknown session is a notification: no
// response, no error, the stream stays usable.
func TestSessionCancelUnknownSessionIgnored(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	id := d.newSession("/tmp/p")
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": "ghost"}})
	// The connection still answers a follow-up request.
	d.request(6, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
	f := d.untilResponseID(6)
	if f["error"] != nil {
		t.Fatalf("connection broken after unknown cancel: %v", f["error"])
	}
	_ = id
}

// --- session/request_permission ------------------------------------------

func TestRequestPermissionRoundTrip(t *testing.T) {
	var outcome acp.RequestPermissionOutcome
	outcomeCh := make(chan acp.RequestPermissionOutcome, 1)
	agent := &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "s1", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			var err error
			outcome, err = out.RequestPermission(ctx,
				acp.ToolCallUpdate{ToolCallID: "c1", Title: new("approve plan")},
				[]acp.PermissionOption{
					{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce},
					{OptionID: "reject-once", Name: "Reject", Kind: acp.PermissionRejectOnce},
				})
			outcomeCh <- outcome
			if err != nil {
				return "", err
			}
			return acp.StopEndTurn, nil
		}}, nil
	}}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{
		"sessionId": id,
		"prompt":    []any{map[string]any{"type": "text", "text": "go"}},
	})

	// The server asks; the client answers allow-once.
	ask := d.frame()
	if ask["method"] != "session/request_permission" {
		t.Fatalf("first frame = %v, want session/request_permission", ask)
	}
	askID := ask["id"].(float64)
	params := ask["params"].(map[string]any)
	if params["sessionId"] != id {
		t.Errorf("permission sessionId = %v", params["sessionId"])
	}
	tc := params["toolCall"].(map[string]any)
	if tc["toolCallId"] != "c1" || tc["title"] != "approve plan" {
		t.Errorf("permission toolCall = %v", tc)
	}
	opts := params["options"].([]any)
	if len(opts) != 2 || opts[0].(map[string]any)["kind"] != acp.PermissionAllowOnce {
		t.Errorf("permission options = %v", opts)
	}
	d.send(map[string]any{
		"jsonrpc": "2.0", "id": int(askID),
		"result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"}},
	})

	select {
	case got := <-outcomeCh:
		if got.Outcome != acp.OutcomeSelected || got.OptionID != "allow-once" {
			t.Errorf("outcome = %+v, want selected/allow-once", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("permission outcome never delivered to the session")
	}
	resp := d.untilResponseID(5)
	if resp["error"] != nil {
		t.Fatalf("prompt errored: %v", resp["error"])
	}
}

// --- protocol-level behavior ---------------------------------------------

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.initialize()
	d.request(9, "tools/list", nil)
	e := respError(t, d.untilResponseID(9))
	if e["code"] != float64(acp.ErrMethodNotFound) {
		t.Errorf("code = %v, want %d", e["code"], acp.ErrMethodNotFound)
	}
}

func TestUnknownNotificationProducesNoFrame(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.send(map[string]any{"jsonrpc": "2.0", "method": "some/unknown-notification"})
	d.request(1, "initialize", map[string]any{"protocolVersion": 1})
	// The next frame must be the initialize response, not an error for
	// the notification.
	f := d.untilResponseID(1)
	if f["error"] != nil {
		t.Fatalf("notification produced a frame or broke the connection: %v", f)
	}
}

func TestParseErrorReturnsCode32700(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.writeMu.Lock()
	_, err := d.w.Write([]byte("{not json}\n"))
	d.writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	f := d.frame()
	e, ok := f["error"].(map[string]any)
	if !ok || e["code"] != float64(acp.ErrParseError) {
		t.Fatalf("expected parse error %d, got %v", acp.ErrParseError, f)
	}
}

// --- golden frames -------------------------------------------------------

func TestUpdateFramesMarshalToSpecShape(t *testing.T) {
	cases := []struct {
		name string
		u    acp.Update
		want string
	}{
		{
			"agent_message_chunk",
			acp.AgentMessageChunk("msg_1", "hello"),
			`{"sessionUpdate":"agent_message_chunk","messageId":"msg_1","content":{"type":"text","text":"hello"}}`,
		},
		{
			"tool_call",
			acp.NewToolCall(acp.ToolCall{ToolCallID: "c1", Title: "add_entity", Kind: acp.ToolKindEdit, Status: acp.ToolStatusPending}),
			`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"add_entity","kind":"edit","status":"pending"}`,
		},
		{
			"tool_call_update",
			acp.ToolCallUpdateFrame(acp.ToolCallUpdate{ToolCallID: "c1", Status: new(acp.ToolStatusCompleted), Content: []acp.ToolCallContent{acp.TextToolContent("ok")}}),
			`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"ok"}}]}`,
		},
		{
			"plan",
			acp.PlanUpdate([]acp.PlanEntry{{Content: "Add posts", Priority: acp.PlanPriorityHigh, Status: acp.PlanStatusPending}}),
			`{"sessionUpdate":"plan","entries":[{"content":"Add posts","priority":"high","status":"pending"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := json.Marshal(tc.u)
			if err != nil {
				t.Fatal(err)
			}
			if string(buf) != tc.want {
				t.Errorf("marshal mismatch:\n got %s\nwant %s", buf, tc.want)
			}
		})
	}
}
