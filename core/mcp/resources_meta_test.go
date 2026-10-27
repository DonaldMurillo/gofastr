package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// A host reads an MCP App's _meta.ui (csp, permissions) from the
// resources/read contents it renders, not from resources/list. The read
// contents carried no _meta, so the declared CSP never reached the host.
func TestResourceReadCarriesMeta(t *testing.T) {
	s := NewServer()
	if err := s.RegisterApp(sampleApp()); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResource("doc://plain", "plain", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "x"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	read := func(uri string) map[string]any {
		params, _ := json.Marshal(map[string]any{"uri": uri})
		res := wireResult(t, s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: params,
		}))
		return res["contents"].([]any)[0].(map[string]any)
	}
	item := read("ui://app/studio.html")
	meta, _ := item["_meta"].(map[string]any)
	ui, _ := meta["ui"].(map[string]any)
	csp, _ := ui["csp"].(map[string]any)
	if got, _ := csp["connectDomains"].([]any); len(got) != 1 || got[0] != "https://api.example.com" {
		t.Fatalf("resources/read contents lack _meta.ui.csp: %v", item)
	}
	if _, ok := read("doc://plain")["_meta"]; ok {
		t.Error("a resource with no Meta carries an empty _meta")
	}
}
