package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// hangingHTTP serves MCP over HTTP in front of an engine whose turns run
// until cancelled. It returns the server URL, the encoder for minting
// bearers, the agent session and its bus.
func hangingHTTP(t *testing.T) (string, *auth.Encoder, ids.SessionID, *engine.Bus) {
	t.Helper()
	mux := multiplex.New()
	cat := resources.NewCatalog()
	sess := ids.NewSessionID()
	bus := engine.NewBus(sess)
	reg := tool.NewRegistry()
	cat.Tools = reg
	cat.Skills = func() []skillmd.Tier1 { return nil }
	eng := engine.NewEngine(sess, bus, hangingProvider{}, "fake", engine.NewDispatcher(bus, reg))
	mux.RegisterEngine(eng)
	cat.RegisterEngine(eng)
	enc := auth.NewEncoder(mustTestSecret(t))
	srv := httptest.NewServer(NewHTTPHandler(New(mux, cat), enc, auth.NewRevocationList()))
	t.Cleanup(func() { srv.Close(); bus.Close() })
	return srv.URL, enc, sess, bus
}

// postMCP sends one JSON-RPC message as bearer on MCP session mcpSess.
func postMCP(ctx context.Context, t *testing.T, url, bearer, mcpSess string, msg map[string]any) (*http.Response, error) {
	t.Helper()
	b, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", mcpSess)
	return http.DefaultClient.Do(req)
}

func cancelMsg(id int) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "method": "notifications/cancelled",
		"params": map[string]any{"requestId": id},
	}
}

// startTurn POSTs a wait=turn call in the background and returns a channel
// closed when its POST answers.
func startTurn(t *testing.T, url, bearer, mcpSess string, sess ids.SessionID) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := postMCP(t.Context(), t, url, bearer, mcpSess, callTool(1, "harness.run_agent_with_shell_access", map[string]any{
			"sessionId": string(sess), "prompt": "hi", "wait": "turn",
		}))
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(150 * time.Millisecond) // let the turn start
	return done
}

func waitTurnEnded(events <-chan control.EventEnvelope, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case env := <-events:
			ev, _ := control.DecodeEvent(env)
			if _, ok := ev.(control.TurnEnded); ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// Issue #614: each POST served its own in-flight table, so the
// notifications/cancelled an MCP client sends as a separate POST found
// nothing and the turn ran on.
func TestHTTPCancelReachesOtherPOST(t *testing.T) {
	url, enc, sess, bus := hangingHTTP(t)
	events := bus.Subscribe(t.Context())
	bearer := "Bearer " + mintHTTPScopeToken(t, enc, nil)
	done := startTurn(t, url, bearer, "mcp-1", sess)

	resp, err := postMCP(t.Context(), t, url, bearer, "mcp-1", cancelMsg(1))
	if err != nil {
		t.Fatalf("cancel POST: %v", err)
	}
	_ = resp.Body.Close()
	if !waitTurnEnded(events, 3*time.Second) {
		t.Fatal("turn still running after a cancel POST")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the cancelled call's POST never answered")
	}
}

// A cancel only reaches requests of the same token principal and MCP
// session: the session id is the client's choice, so it alone proves
// nothing.
func TestHTTPCancelOtherOwnerIgnored(t *testing.T) {
	url, enc, sess, bus := hangingHTTP(t)
	events := bus.Subscribe(t.Context())
	owner := "Bearer " + mintHTTPScopeToken(t, enc, nil)
	stranger := "Bearer " + mintHTTPScopeToken(t, enc, nil)
	startTurn(t, url, owner, "mcp-1", sess)

	for _, c := range []struct{ bearer, mcpSess string }{{stranger, "mcp-1"}, {owner, "mcp-2"}} {
		resp, err := postMCP(t.Context(), t, url, c.bearer, c.mcpSess, cancelMsg(1))
		if err != nil {
			t.Fatalf("cancel POST: %v", err)
		}
		_ = resp.Body.Close()
	}
	if waitTurnEnded(events, 400*time.Millisecond) {
		t.Fatal("SECURITY: a cancel from another principal or session ended the turn")
	}
	resp, err := postMCP(t.Context(), t, url, owner, "mcp-1", cancelMsg(1))
	if err != nil {
		t.Fatalf("cancel POST: %v", err)
	}
	_ = resp.Body.Close()
	if !waitTurnEnded(events, 3*time.Second) {
		t.Fatal("the owner's cancel did not end the turn")
	}
}

// A second POST reusing a running request's id is refused, so a cancel
// names one call.
func TestHTTPDuplicateIDAcrossPOSTs(t *testing.T) {
	url, enc, sess, _ := hangingHTTP(t)
	bearer := "Bearer " + mintHTTPScopeToken(t, enc, nil)
	startTurn(t, url, bearer, "mcp-1", sess)

	resp, err := postMCP(t.Context(), t, url, bearer, "mcp-1", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m["error"] == nil {
		t.Fatalf("reused id answered across POSTs: %v", m)
	}
	_, _ = postMCP(t.Context(), t, url, bearer, "mcp-1", cancelMsg(1))
}

// The per-owner cap counts every connection's requests; another owner
// keeps its own share.
func TestInflightCapPerOwner(t *testing.T) {
	tab := newInflightTable()
	a := inflightOwner{principal: "p1", session: "s1"}
	for i := range maxInFlight {
		id, _ := json.Marshal(i)
		if _, _, err := tab.track(context.Background(), a, id); err != nil {
			t.Fatalf("request %d refused: %v", i, err)
		}
	}
	if _, _, err := tab.track(context.Background(), a, json.RawMessage(`"x"`)); err != errTooManyInFlight {
		t.Fatalf("request past the cap: err = %v, want errTooManyInFlight", err)
	}
	b := inflightOwner{principal: "p2", session: "s1"}
	_, release, err := tab.track(context.Background(), b, json.RawMessage(`"x"`))
	if err != nil {
		t.Fatalf("another owner refused: %v", err)
	}
	release()
	if _, ok := tab.count[b]; ok {
		t.Fatal("a released owner's count stayed in the table")
	}
}
