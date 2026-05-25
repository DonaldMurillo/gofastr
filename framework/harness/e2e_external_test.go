//go:build e2e_real

// e2e external-communication tests — drive a real turn through every
// transport the harness exposes: MCP stdio, MCP streamable HTTP, WS
// command/event frames, REST POST + SSE, and the MCP client (consumer).
//
// All scripted; no LLM calls. Run with:
//
//   go test -tags=e2e_real -v -run E2EExternal ./framework/harness -count=1

package harness

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/mcpserver"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/rest"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/ws"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/mcpclient"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// ---------- 1. MCP stdio: full tools/call cycle ----------

// TestE2EExternal_MCPStdio_RunAgentWithShellAccess drives a turn
// through the MCP server's tools/call surface. The honest tool is
// invoked with wait=turn; we verify the synchronous text + meta
// payload return.
func TestE2EExternal_MCPStdio_RunAgentWithShellAccess(t *testing.T) {
	h, sess, cleanup := plumbingHarnessWithRealTools(t, &scriptedProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "agent-via-mcp says hi"},
			{Kind: provider.KindUsage, Usage: &provider.Usage{InputTokens: 5, OutputTokens: 3}},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	})
	defer cleanup()

	cat := resources.NewCatalog()
	cat.Tools = h.Tools
	cat.Providers = h.Providers
	cat.Skills = func() []skillmd.Tier1 { return nil }
	cat.RegisterEngine(h.Mux.EngineFor(sess))
	srv := mcpserver.New(h.Mux, cat)

	// Marshal a tools/call request.
	args, _ := json.Marshal(map[string]any{
		"sessionId": string(sess),
		"prompt":    "ping",
		"wait":      "turn",
	})
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "tools/call",
		"params": map[string]any{
			"name":      "harness.run_agent_with_shell_access",
			"arguments": json.RawMessage(args),
		},
	})
	in := strings.NewReader(string(req) + "\n")
	var out strings.Builder
	srv.WithIO(in, &mcpWriteAdapter{b: &out})
	if err := srv.Serve(context.Background()); err != nil {
		t.Logf("Serve returned: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("bad response JSON: %v\nraw=%s", err, out.String())
	}
	if resp["error"] != nil {
		t.Fatalf("MCP error: %v", resp["error"])
	}
	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("empty content: %v", resp)
	}
	text := content[0].(map[string]any)["text"].(string)
	if text != "agent-via-mcp says hi" {
		t.Errorf("text = %q, want full assistant text", text)
	}
	meta, _ := result["_meta"].(map[string]any)
	if meta == nil {
		t.Errorf("missing _meta with cost/turns/toolCalls")
	} else {
		if _, ok := meta["cost"]; !ok {
			t.Errorf("_meta missing cost")
		}
		if turns, ok := meta["turns"].(float64); !ok || turns < 1 {
			t.Errorf("_meta.turns = %v, want >= 1", meta["turns"])
		}
	}
}

// ---------- 2. MCP stdio: resources/read returns live sessions ----------

// TestE2EExternal_MCPStdio_ResourcesRead verifies the resources/read
// path: ask for harness/v1://sessions, get a JSON blob listing the
// real session we registered.
func TestE2EExternal_MCPStdio_ResourcesRead(t *testing.T) {
	h, sess, cleanup := plumbingHarnessWithRealTools(t, &scriptedProvider{})
	defer cleanup()
	cat := resources.NewCatalog()
	cat.Tools = h.Tools
	cat.Providers = h.Providers
	cat.Skills = func() []skillmd.Tier1 { return nil }
	cat.RegisterEngine(h.Mux.EngineFor(sess))
	srv := mcpserver.New(h.Mux, cat)

	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "resources/read",
		"params": map[string]any{"uri": "harness/v1://sessions"},
	})
	in := strings.NewReader(string(req) + "\n")
	var out strings.Builder
	srv.WithIO(in, &mcpWriteAdapter{b: &out})
	_ = srv.Serve(context.Background())

	var resp map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp)
	result, _ := resp["result"].(map[string]any)
	contents, _ := result["contents"].([]any)
	if len(contents) == 0 {
		t.Fatalf("no contents in resources/read: %v", resp)
	}
	first := contents[0].(map[string]any)
	if first["uri"] != "harness/v1://sessions" {
		t.Errorf("URI = %q", first["uri"])
	}
	if !strings.Contains(first["text"].(string), string(sess)) {
		t.Errorf("session %s not in resources/read body: %s", sess, first["text"])
	}
}

// ---------- 3. MCP streamable HTTP: initialize + tools/call ----------

// TestE2EExternal_MCPHTTP_InitializeAndToolCall hits the streamable
// HTTP wrapper and runs an agent turn through it.
func TestE2EExternal_MCPHTTP_InitializeAndToolCall(t *testing.T) {
	h, sess, cleanup := plumbingHarnessWithRealTools(t, &scriptedProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "via http"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	})
	defer cleanup()
	cat := resources.NewCatalog()
	cat.Tools = h.Tools
	cat.Providers = h.Providers
	cat.Skills = func() []skillmd.Tier1 { return nil }
	cat.RegisterEngine(h.Mux.EngineFor(sess))
	stdio := mcpserver.New(h.Mux, cat)

	httpSrv := httptest.NewServer(mcpserver.NewHTTPHandler(stdio, nil, nil))
	defer httpSrv.Close()

	// initialize
	postJSON(t, httpSrv.URL+"/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "initialize")

	// tools/call
	args, _ := json.Marshal(map[string]any{
		"sessionId": string(sess),
		"prompt":    "ping",
		"wait":      "turn",
	})
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"harness.run_agent_with_shell_access","arguments":%s}}`, string(args))
	respText := postJSON(t, httpSrv.URL+"/mcp", body, "tools/call")
	if !strings.Contains(respText, "via http") {
		t.Errorf("HTTP MCP tool result missing text: %q", respText)
	}
}

// ---------- 4. WS: real command/event frame exchange ----------

// TestE2EExternal_WS_CommandAndEventFrames opens a WS connection,
// sends a SendInput command frame, receives at least one TextDelta
// event frame.
func TestE2EExternal_WS_CommandAndEventFrames(t *testing.T) {
	h, sess, cleanup := plumbingHarnessWithRealTools(t, &scriptedProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "ws-frame-reply"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	})
	defer cleanup()

	secret, _ := auth.GenerateSecret()
	enc := auth.NewEncoder(secret)
	tok, _ := enc.Encode(auth.Claims{
		Sessions:      []ids.SessionID{sess},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	srv := httptest.NewServer(&ws.Handler{
		Mux: h.Mux, Encoder: enc, Revocations: auth.NewRevocationList(),
	})
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Handshake.
	hs := fmt.Sprintf(
		"GET /?session=%s&token=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n",
		sess, tok, host,
	)
	if _, err := conn.Write([]byte(hs)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	br := bufio.NewReader(conn)
	// Drain response headers until blank line.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// Send a SendInput command frame.
	cmd := control.SendInput{SessionID: sess, Content: engine.SimpleInput("ping")}
	cmdBody, _ := control.MarshalCommand(cmd)
	frame := wsClientTextFrame(t, mustJSON(map[string]any{
		"frame": "command",
		"body":  json.RawMessage(cmdBody),
	}))
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}

	// Read frames until we see a TextDelta payload.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	sawTextDelta := false
	for !sawTextDelta {
		op, payload, err := readServerFrame(br)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if op != 0x1 { // not text
			continue
		}
		var f struct {
			Frame string          `json:"frame"`
			Body  json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(payload, &f); err != nil {
			continue
		}
		if f.Frame != "event" {
			continue
		}
		if strings.Contains(string(f.Body), `"kind":"TextDelta"`) {
			sawTextDelta = true
		}
	}
}

// ---------- 5. REST: POST input + SSE event stream ----------

// TestE2EExternal_REST_PostInputAndSSE drives a real turn via the
// REST transport: POST /v1/sessions/{id}/input and consume
// /v1/sessions/{id}/events SSE, verify TextDelta arrives.
func TestE2EExternal_REST_PostInputAndSSE(t *testing.T) {
	h, sess, cleanup := plumbingHarnessWithRealTools(t, &scriptedProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "rest-sse-reply"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	})
	defer cleanup()

	secret, _ := auth.GenerateSecret()
	enc := auth.NewEncoder(secret)
	cat := resources.NewCatalog()
	cat.Tools = h.Tools
	cat.Providers = h.Providers
	cat.Skills = func() []skillmd.Tier1 { return nil }
	cat.RegisterEngine(h.Mux.EngineFor(sess))

	server := &rest.Server{
		Mux: h.Mux, Catalog: cat, Encoder: enc,
		Revocations: auth.NewRevocationList(),
		Features:    []string{"rest"},
	}
	httpSrv := httptest.NewServer(server.Handler())
	defer httpSrv.Close()

	tok, _ := enc.Encode(auth.Claims{
		Sessions:      []ids.SessionID{sess},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})

	// Open SSE stream first so we don't miss the early events.
	sseReq, _ := http.NewRequest("GET", httpSrv.URL+"/v1/sessions/"+string(sess)+"/events", nil)
	sseReq.Header.Set("X-Harness-Token", tok)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != 200 {
		t.Fatalf("SSE status = %d", sseResp.StatusCode)
	}

	// Brief pause so the SSE subscription is registered on the engine bus.
	time.Sleep(50 * time.Millisecond)

	// POST input.
	postBody, _ := json.Marshal(control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput("ping"),
	})
	postReq, _ := http.NewRequest("POST", httpSrv.URL+"/v1/sessions/"+string(sess)+"/input",
		strings.NewReader(string(postBody)))
	postReq.Header.Set("X-Harness-Token", tok)
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST input status = %d", postResp.StatusCode)
	}

	// Read SSE until we see a TextDelta event.
	br := bufio.NewReader(sseResp.Body)
	deadline := time.Now().Add(3 * time.Second)
	sawText := false
	for time.Now().Before(deadline) && !sawText {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "event: TextDelta") {
			sawText = true
		}
	}
	if !sawText {
		t.Fatal("never received TextDelta SSE event after POST /input")
	}
}

// ---------- 6. MCP client (consumer) bridges a real stub server ----------

// TestE2EExternal_MCPClient_BridgesStubServer spawns a shell-script
// MCP server, has the harness's mcpclient.Client connect, list its
// tools, and call one. Verifies the consumer side of MCP.
func TestE2EExternal_MCPClient_BridgesStubServer(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *initialize*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}\n' "$id"
      ;;
    *tools/list*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"reflect","description":"echoes back","inputSchema":{"type":"object"}}]}}\n' "$id"
      ;;
    *tools/call*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"stub-tool-says-hi"}]}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := mcpclient.Spawn(ctx, stub, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	src := mcpclient.NewSource("stub", "eager", c)
	reg := tool.NewRegistry()
	if err := reg.Register(ctx, src); err != nil {
		t.Fatal(err)
	}
	got := reg.List()
	if len(got) != 1 || got[0].Name() != "stub.reflect" {
		t.Fatalf("registry list = %v", got)
	}

	// Invoke it.
	res, err := got[0].Run(ctx, tool.ToolCall{
		ID:    ids.NewCallID(),
		Name:  "stub.reflect",
		Input: json.RawMessage(`{}`),
	}, &nopSink{})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("bridged tool returned error: %+v", res)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "stub-tool-says-hi") {
		t.Errorf("bridged tool result missing text: %+v", res.Content)
	}
}

// ---------- 7. MCP subprocess: tools/list contains the honest name ----------
// (Covered by TestE2EPlumbing_MCPServer_Subprocess; included here for completeness.)

// ---------- helpers ----------

type mcpWriteAdapter struct{ b *strings.Builder }

func (w *mcpWriteAdapter) Write(p []byte) (int, error) { w.b.Write(p); return len(p), nil }

type nopSink struct{}

func (nopSink) EmitProgress(_ string)       {}
func (nopSink) EmitEvent(_ control.Event)   {}

func postJSON(t *testing.T, url, body, label string) string {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s POST: %v", label, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.Fatalf("%s status %d: %s", label, resp.StatusCode, out)
	}
	return string(out)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// wsClientTextFrame builds a masked client-side text frame (mask bit
// required for client→server frames per RFC 6455).
func wsClientTextFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	mask := []byte{0xAB, 0xCD, 0xEF, 0x12}
	header := []byte{0x80 | 0x1} // FIN + text
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload))|0x80)
	case len(payload) <= 0xFFFF:
		header = append(header, 126|0x80)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		header = append(header, ext[:]...)
	default:
		header = append(header, 127|0x80)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		header = append(header, ext[:]...)
	}
	header = append(header, mask...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	return append(header, masked...)
}

func readServerFrame(br *bufio.Reader) (op byte, payload []byte, err error) {
	b0, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b1, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	op = b0 & 0x0F
	length := uint64(b1 & 0x7F)
	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	return op, payload, nil
}

// unused-import paranoia
var _ = exec.Command
