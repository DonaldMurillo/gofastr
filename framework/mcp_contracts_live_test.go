package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The handler-level behaviour of the contract tools is covered in
// contracts_docs_test.go, which calls the handlers directly. These tests
// cover the transport instead: the tools are only reachable by an agent
// if WithMCP mounts /mcp and the JSON-RPC layer serialises them. That
// path is genuinely separate, registration happens in InitPlugins but
// the mount happens in Start, so a tool can register cleanly and still
// 404 for every client.

// mcpCall drives one JSON-RPC request against the live /mcp endpoint and
// returns the decoded envelope's text content. Listed tools are flattened
// as "name\x00description" lines so one helper serves both methods.
func mcpCall(t *testing.T, app *App, body string) (text string, isError bool) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/mcp returned HTTP %d: %s", rec.Code, rec.Body.String())
	}
	payload := rec.Body.String()
	if i := strings.Index(payload, "{"); i > 0 {
		payload = payload[i:] // strip an SSE "data: " framing prefix
	}
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if resp.Error != nil {
		return resp.Error.Message, true
	}
	for _, tl := range resp.Result.Tools {
		text += tl.Name + "\x00" + tl.Description + "\n"
	}
	for _, c := range resp.Result.Content {
		text += c.Text
	}
	return text, resp.Result.IsError
}

func newLiveMCPApp(t *testing.T) *App {
	t.Helper()
	app, cleanup := startApp(t, NewApp(WithConfig(AppConfig{Name: "mcp-live"}), WithMCP(), WithMCPIntrospection()))
	t.Cleanup(cleanup)
	return app
}

func TestContractToolsAreListedOverLiveMCP(t *testing.T) {
	app := newLiveMCPApp(t)

	out, isErr := mcpCall(t, app, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if isErr {
		t.Fatalf("tools/list: %s", out)
	}
	for _, want := range []string{"contracts_list", "contracts_explain", "contracts_capabilities"} {
		i := strings.Index(out, want+"\x00")
		if i < 0 {
			t.Errorf("%s is not listed over /mcp", want)
			continue
		}
		desc := out[i+len(want)+1:]
		if j := strings.IndexByte(desc, '\n'); j >= 0 {
			desc = desc[:j]
		}
		// A tool an agent cannot tell apart from its siblings is a tool
		// it will not reach for.
		if len(desc) < 40 {
			t.Errorf("%s has a description too thin to route on: %q", want, desc)
		}
	}
}

// Listing a tool proves nothing about calling it. This drives a real
// tools/call and asserts the payload carries the fields an agent needs
// to act: why it matters, the fix, and where to read more.
func TestContractsExplainReturnsActionableContentOverMCP(t *testing.T) {
	app := newLiveMCPApp(t)

	out, isErr := mcpCall(t, app, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"contracts_explain","arguments":{"rule":"GOFASTR1002"}}}`)
	if isErr {
		t.Fatalf("contracts_explain: %s", out)
	}
	for _, want := range []string{"GOFASTR1002", `"why"`, `"fix"`, `"docUrl"`} {
		if !strings.Contains(out, want) {
			t.Errorf("contracts_explain payload is missing %s:\n%s", want, out)
		}
	}
}
