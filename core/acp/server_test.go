package acp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	newFn  func(ctx context.Context, cwd string) (acp.Session, error)
	minted atomic.Int64
}

func (a *fakeAgent) Info() acp.Implementation {
	return acp.Implementation{Name: "fake", Title: "Fake Agent", Version: "1.2.3"}
}

func (a *fakeAgent) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	if a.newFn != nil {
		return a.newFn(ctx, cwd)
	}
	// Each session gets its own id, as the Agent contract requires.
	return &fakeSession{id: fmt.Sprintf("sess_%d", a.minted.Add(1))}, nil
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
	buf = normalizeTestCWDs(buf)
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
		if !filepath.IsAbs(cwd) {
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

// A load can ask permission, through its own Client or one kept from
// earlier: the reader keeps delivering answers while the load runs. It
// used to run the load itself, so the answer was never read and the
// connection hung.
func TestLoadCanAskPermission(t *testing.T) {
	agent := &fakeLoadingAgent{}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")

	outcomes := make(chan acp.RequestPermissionOutcome, 2)
	var kept *acp.Client
	agent.loadFn = func(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
		// The second load asks through the Client the first one kept.
		asker := out
		if kept != nil {
			asker = kept
		}
		kept = out
		got, err := asker.RequestPermission(ctx, acp.ToolCallUpdate{ToolCallID: "c1"},
			[]acp.PermissionOption{{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce}})
		if err != nil {
			return nil, err
		}
		outcomes <- got
		return &fakeSession{id: sid}, nil
	}
	for _, loadID := range []int{4, 6} {
		d.request(loadID, "session/load", map[string]any{"sessionId": id, "cwd": "/tmp/p", "mcpServers": []any{}})
		ask := d.frame()
		if ask["method"] != "session/request_permission" {
			t.Fatalf("frame = %v, want session/request_permission", ask)
		}
		// A request sent while the load waits is answered after it, in
		// the order it was read.
		d.request(loadID+1, "session/new", map[string]any{"cwd": "/tmp/q", "mcpServers": []any{}})
		d.send(map[string]any{
			"jsonrpc": "2.0", "id": int(ask["id"].(float64)),
			"result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"}},
		})
		if r := d.frame(); r["id"] != float64(loadID) || r["error"] != nil {
			t.Fatalf("frame = %v, want the session/load %d result", r, loadID)
		}
		if got := <-outcomes; got.Outcome != acp.OutcomeSelected {
			t.Errorf("load %d: outcome %+v, want selected", loadID, got)
		}
		if r := d.untilResponseID(loadID + 1); r["error"] != nil {
			t.Fatalf("session/new after load errored: %v", r["error"])
		}
	}
}

// askingLoad is a loader that asks permission and waits for the answer.
func askingLoad(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
	if _, err := out.RequestPermission(ctx, acp.ToolCallUpdate{ToolCallID: "c1"},
		[]acp.PermissionOption{{OptionID: "allow-once", Name: "Allow once", Kind: acp.PermissionAllowOnce}}); err != nil {
		return nil, err
	}
	return &fakeSession{id: sid}, nil
}

// A load waiting on its context ends when the input does. EOF failed
// only the pending permission request, and the load's context lived on
// until Serve returned, which waited for the load.
func TestLoadEndsAtEOF(t *testing.T) {
	started := make(chan struct{})
	agent := &fakeLoadingAgent{loadFn: func(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	d := startDialog(t, agent, nil)
	d.initialize()
	d.request(2, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
	<-started
	d.w.Close()
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve stayed in the load after EOF")
	}
}

// A request held behind a waiting load does not start after EOF: it
// ran with a live context and nobody left to answer, so Serve waited
// on it forever.
func TestHeldLoadDroppedAtEOF(t *testing.T) {
	started := make(chan string, 2)
	agent := &fakeLoadingAgent{loadFn: func(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
		started <- sid
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	d := startDialog(t, agent, nil)
	d.initialize()
	d.request(2, "session/load", map[string]any{"sessionId": "a", "cwd": "/tmp/p", "mcpServers": []any{}})
	<-started
	d.request(3, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
	// The second load is read and held before the input ends.
	d.request(4, "session/new", map[string]any{"cwd": "/tmp/q", "mcpServers": []any{}})
	time.Sleep(50 * time.Millisecond)
	d.w.Close()
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve ran a held load after EOF and waited on it")
	}
}

// A cancel reaches a running turn while another request waits on the
// client: held behind a load asking permission, it waited for the
// answer the user might give only after the turn stopped.
func TestCancelPassesWaitingLoad(t *testing.T) {
	started := make(chan struct{})
	agent := &fakeLoadingAgent{loadFn: askingLoad}
	agent.newFn = func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "a", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			close(started)
			<-ctx.Done()
			return acp.StopEndTurn, nil
		}}, nil
	}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	d.request(5, "session/prompt", map[string]any{"sessionId": id, "prompt": []any{map[string]any{"type": "text", "text": "slow"}}})
	<-started
	d.request(6, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
	ask := d.frame()
	if ask["method"] != "session/request_permission" {
		t.Fatalf("frame = %v, want session/request_permission", ask)
	}
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": id}})
	if got := respResult(t, d.untilResponseID(5))["stopReason"]; got != acp.StopCancelled {
		t.Errorf("stopReason = %v, want cancelled", got)
	}
	d.send(map[string]any{"jsonrpc": "2.0", "id": int(ask["id"].(float64)),
		"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
	d.untilResponseID(6)
}

// A cancel sent after a prompt that is still waiting behind a load
// cancels that prompt once it starts.
func TestCancelFollowsHeldPrompt(t *testing.T) {
	agent := &fakeLoadingAgent{loadFn: askingLoad}
	agent.newFn = func(ctx context.Context, cwd string) (acp.Session, error) {
		return &fakeSession{id: "a", promptFn: func(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
			<-ctx.Done()
			return acp.StopEndTurn, nil
		}}, nil
	}
	d := startDialog(t, agent, nil)
	id := d.newSession("/tmp/p")
	d.request(6, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
	ask := d.frame()
	if ask["method"] != "session/request_permission" {
		t.Fatalf("frame = %v, want session/request_permission", ask)
	}
	d.request(5, "session/prompt", map[string]any{"sessionId": id, "prompt": []any{map[string]any{"type": "text", "text": "slow"}}})
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": id}})
	d.send(map[string]any{"jsonrpc": "2.0", "id": int(ask["id"].(float64)),
		"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
	if got := respResult(t, d.untilResponseID(5))["stopReason"]; got != acp.StopCancelled {
		t.Errorf("stopReason = %v, want cancelled", got)
	}
}

// Frames held behind a waiting request are bounded by count and by
// bytes. Past the bound a request is refused at once and reading goes
// on, so the permission answer still gets through.
func TestHeldFramesBounded(t *testing.T) {
	for name, pad := range map[string]int{"count": 0, "bytes": 3 << 20} {
		t.Run(name, func(t *testing.T) {
			d := startDialog(t, &fakeLoadingAgent{loadFn: askingLoad}, nil)
			d.initialize()
			d.request(2, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
			ask := d.frame()
			if ask["method"] != "session/request_permission" {
				t.Fatalf("frame = %v, want session/request_permission", ask)
			}
			n := 300
			if pad > 0 {
				n = 8
			}
			go func() {
				for i := range n {
					d.request(100+i, "session/new", map[string]any{"cwd": "/tmp/q", "mcpServers": []any{}, "pad": strings.Repeat("x", pad)})
				}
			}()
			// Refusals arrive while the load still waits.
			first := d.frame()
			if e := respError(t, first); e["code"] != float64(acp.ErrInternalError) {
				t.Fatalf("refusal code = %v", e["code"])
			}
			d.send(map[string]any{"jsonrpc": "2.0", "id": int(ask["id"].(float64)),
				"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
			// Every request gets one answer: the load, then each
			// session/new, refused or run.
			seen := map[float64]bool{first["id"].(float64): true}
			for len(seen) < n {
				if id, _ := d.frame()["id"].(float64); id >= 100 {
					seen[id] = true
				}
			}
		})
	}
}

// The byte bound counts frames still held, not every frame ever held:
// two waits holding 12 MiB each both run every request.
func TestHeldBytesReleased(t *testing.T) {
	d := startDialog(t, &fakeLoadingAgent{loadFn: askingLoad}, nil)
	d.initialize()
	for round := range 2 {
		loadID := 10 * (round + 1)
		d.request(loadID, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})
		ask := d.frame()
		if ask["method"] != "session/request_permission" {
			t.Fatalf("frame = %v, want session/request_permission", ask)
		}
		go func() {
			for i := range 4 {
				d.request(loadID+1+i, "session/new", map[string]any{"cwd": "/tmp/q", "mcpServers": []any{}, "pad": strings.Repeat("x", 3<<20)})
			}
			d.send(map[string]any{"jsonrpc": "2.0", "id": int(ask["id"].(float64)),
				"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
		}()
		for seen := 0; seen < 5; {
			r := d.frame()
			if r["error"] != nil && r["id"] != float64(loadID) {
				t.Fatalf("round %d: frame %v refused", round, r)
			}
			if id, _ := r["id"].(float64); id >= float64(loadID) && id <= float64(loadID+4) {
				seen++
			}
		}
	}
}

// A client that pipelines requests behind a slow one is never refused:
// with nothing asked of the client, a full hold stops reading, and the
// pipe holds the client back. The refusal is for a request that waits on
// the client's answer while the client keeps sending.
func TestPipelinedRequestsWait(t *testing.T) {
	// The count case also checks the bound holds: the client's writes
	// stall. The bytes case fits in what the pipe and scanner take in.
	for name, c := range map[string]struct {
		sent, pad int
		stalls    bool
	}{"count": {300, 0, true}, "bytes": {8, 3 << 20, false}} {
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			var n atomic.Int64
			d := startDialog(t, &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
				if n.Add(1) == 1 {
					<-release
				}
				return &fakeSession{id: fmt.Sprintf("p%d", n.Load())}, nil
			}}, nil)
			d.initialize()

			var wrote atomic.Int64
			go func() {
				for i := range c.sent {
					d.request(100+i, "session/new", map[string]any{"cwd": "/tmp/p", "pad": strings.Repeat("x", c.pad)})
					wrote.Add(1)
				}
			}()
			waitForStall(&wrote, c.sent)
			if got := wrote.Load(); c.stalls && got >= int64(c.sent) {
				t.Errorf("the client wrote all %d frames behind a stuck request; the hold has no bound", got)
			}
			close(release)
			for range c.sent {
				if f := d.frame(); f["error"] != nil {
					t.Fatalf("pipelined request %v refused: %v", f["id"], f["error"])
				}
			}
		})
	}
}

// waitForStall gives the server a second to read what it will of a
// flood the client is still writing.
func waitForStall(wrote *atomic.Int64, sent int) {
	for deadline := time.Now().Add(time.Second); wrote.Load() < int64(sent) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
}

// A request that asks the client something after the read paused on a
// full hold still gets its answer: the ask wakes the reader, which reads
// on, refusing what is past the bound, until the answer arrives.
func TestFullHoldWakesForAnswer(t *testing.T) {
	release := make(chan struct{})
	d := startDialog(t, &fakeLoadingAgent{loadFn: func(ctx context.Context, sid, cwd string, out *acp.Client) (acp.Session, error) {
		<-release
		return askingLoad(ctx, sid, cwd, out)
	}}, nil)
	d.initialize()
	d.request(2, "session/load", map[string]any{"sessionId": "b", "cwd": "/tmp/p", "mcpServers": []any{}})

	const sent = 300
	var wrote atomic.Int64
	go func() {
		for i := range sent {
			d.request(100+i, "session/new", map[string]any{"cwd": "/tmp/q", "mcpServers": []any{}})
			wrote.Add(1)
		}
	}()
	waitForStall(&wrote, sent)
	close(release)
	// Every request gets one answer: the load, then each session/new,
	// refused or run.
	loaded := false
	for answered := 0; !loaded || answered < sent; {
		f := d.frame()
		switch id, _ := f["id"].(float64); {
		case f["method"] == "session/request_permission":
			go d.send(map[string]any{"jsonrpc": "2.0", "id": int(id),
				"result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
		case id == 2:
			respResult(t, f)
			loaded = true
		case id >= 100:
			answered++
		}
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

// An Agent whose NewSession hands back an id this connection already
// holds broke its contract: session/new refuses it and the running
// session keeps its busy flag and cancel handle. Replacing the entry
// let a second prompt run beside the first and left session/cancel
// unable to reach the running turn.
func TestNewSessionRefusesHeldID(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts *acp.Options
		// evict mints a second id first, so EvictOldest drops the busy
		// s1 into draining before the duplicate arrives.
		evict bool
	}{
		{name: "live"},
		{name: "draining", opts: &acp.Options{MaxSessions: 1, SessionOverflow: acp.SessionOverflowEvictOldest}, evict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var minted atomic.Int64
			started, release := make(chan struct{}), make(chan struct{})
			agent := &fakeAgent{newFn: func(ctx context.Context, cwd string) (acp.Session, error) {
				n := minted.Add(1)
				if tc.evict && n == 2 {
					return &fakeSession{id: "s2"}, nil
				}
				return &fakeSession{id: "s1", promptFn: func(ctx context.Context, _ []acp.ContentBlock, _ *acp.Client) (string, error) {
					if n == 1 {
						close(started)
						if tc.evict {
							<-release // an evicted turn that ignores its cancel drains
						} else {
							<-ctx.Done()
						}
					}
					return acp.StopEndTurn, nil
				}}, nil
			}}
			d := startDialog(t, agent, tc.opts)
			defer close(release)
			id := d.newSession("/tmp/p")
			d.request(5, "session/prompt", map[string]any{
				"sessionId": id,
				"prompt":    []any{map[string]any{"type": "text", "text": "slow"}},
			})
			<-started
			if tc.evict {
				d.request(6, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
				respResult(t, d.untilResponseID(6))
			}
			d.request(7, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
			if e := respError(t, d.untilResponseID(7)); e["code"] != float64(acp.ErrInternalError) {
				t.Fatalf("duplicate session id error = %v, want internal error", e)
			}
			if tc.evict {
				return
			}
			d.request(8, "session/prompt", map[string]any{
				"sessionId": id,
				"prompt":    []any{map[string]any{"type": "text", "text": "again"}},
			})
			if e := respError(t, d.untilResponseID(8)); e["code"] != float64(acp.ErrInvalidRequest) {
				t.Errorf("second prompt on the busy session = %v, want a turn-in-progress refusal", e)
			}
			d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": id}})
			if got := respResult(t, d.untilResponseID(5))["stopReason"]; got != acp.StopCancelled {
				t.Errorf("stopReason = %v, want cancelled: session/cancel lost the running turn", got)
			}
		})
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
		{
			// An empty plan still carries entries: the schema requires the
			// field, and a client reads it as the plan now being empty.
			"plan_empty",
			acp.PlanUpdate(nil),
			`{"sessionUpdate":"plan","entries":[]}`,
		},
		{
			// A patch that sets a field to empty clears it: an absent
			// field means "unchanged", so the empty value must be sent.
			"tool_call_update_clears",
			acp.ToolCallUpdateFrame(acp.ToolCallUpdate{ToolCallID: "c1", Title: new(""), Content: []acp.ToolCallContent{}, RawInput: map[string]any{}, RawOutput: map[string]any{}}),
			`{"sessionUpdate":"tool_call_update","toolCallId":"c1","title":"","content":[],"rawInput":{},"rawOutput":{}}`,
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
