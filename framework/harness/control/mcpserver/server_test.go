package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "mcp-hello"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// runMCPRequest sends one JSON-RPC request to the server (via a
// pre-buffered reader) and parses one response.
func runMCPRequest(t *testing.T, s *Server, req map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(req)
	in := bytes.NewBuffer(body)
	in.WriteByte('\n')
	var out bytes.Buffer
	s.WithIO(in, &out)
	if err := s.Serve(context.Background()); err != nil {
		t.Logf("Serve returned: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("parse response: %v\nraw=%q", err, out.String())
	}
	return resp
}

func newTestServer(t *testing.T) (*Server, ids.SessionID, *multiplex.Mux) {
	t.Helper()
	mux := multiplex.New()
	cat := resources.NewCatalog()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	cat.Tools = reg
	cat.Providers = []provider.Provider{fakeProvider{}}
	cat.Skills = func() []skillmd.Tier1 { return nil }
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)
	mux.RegisterEngine(eng)
	cat.RegisterEngine(eng)
	t.Cleanup(func() { bus.Close() })
	s := New(mux, cat)
	return s, session, mux
}

func TestMCPInitialize(t *testing.T) {
	s, _, _ := newTestServer(t)
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "initialize",
		"params": map[string]any{},
	})
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", resp)
	}
	if result["protocolVersion"] == nil {
		t.Errorf("missing protocolVersion")
	}
}

func TestMCPToolsListIncludesHonestlyNamedTool(t *testing.T) {
	s, _, _ := newTestServer(t)
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 2,
		"method": "tools/list",
	})
	tools, _ := resp["result"].(map[string]any)["tools"].([]any)
	found := false
	for _, raw := range tools {
		entry := raw.(map[string]any)
		if entry["name"] == "harness.run_agent_with_shell_access" {
			found = true
			desc := entry["description"].(string)
			if !strings.Contains(desc, "Bash") {
				t.Errorf("description should warn about Bash: %q", desc)
			}
		}
	}
	if !found {
		t.Errorf("expected run_agent_with_shell_access tool")
	}
}

func TestMCPRunAgentWithShellAccess(t *testing.T) {
	s, session, _ := newTestServer(t)
	args, _ := json.Marshal(map[string]any{
		"sessionId": string(session),
		"prompt":    "hi",
		"wait":      "turn",
	})
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 7,
		"method": "tools/call",
		"params": map[string]any{
			"name":      "harness.run_agent_with_shell_access",
			"arguments": json.RawMessage(args),
		},
	})
	if resp["error"] != nil {
		t.Fatalf("error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("empty content")
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "mcp-hello") {
		t.Errorf("text = %q, want mcp-hello", text)
	}
}

func TestMCPResourcesList(t *testing.T) {
	s, _, _ := newTestServer(t)
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 3,
		"method": "resources/list",
	})
	resources := resp["result"].(map[string]any)["resources"].([]any)
	if len(resources) == 0 {
		t.Fatal("no resources listed")
	}
	uri := resources[0].(map[string]any)["uri"].(string)
	if !strings.HasPrefix(uri, "harness/v1://") {
		t.Errorf("URI scheme = %q", uri)
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	s, _, _ := newTestServer(t)
	resp := runMCPRequest(t, s, map[string]any{
		"jsonrpc": "2.0", "id": 99,
		"method": "mystery/op",
	})
	if resp["error"] == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestMCPRequiredTokenRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.RequiredToken = "expected-tok"
	// Don't set GOFASTR_HARNESS_TOKEN.
	var out bytes.Buffer
	s.WithIO(bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n"), &out)
	err := s.Serve(context.Background())
	if err == nil {
		t.Fatal("expected error when token mismatched")
	}
	_ = time.Second // keep time import (race-tolerance)
}
