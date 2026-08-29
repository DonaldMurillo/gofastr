package mcp

import (
	"context"
	"strings"
	"testing"
)

// mustRegisterTemplate registers a resource template or fails the test.
func mustRegisterTemplate(t *testing.T, s *Server, uriTemplate, name string, opts ...ResourceTemplateOption) {
	t.Helper()
	if err := s.RegisterResourceTemplate(uriTemplate, name, "text/markdown", opts...); err != nil {
		t.Fatalf("register template %s: %v", uriTemplate, err)
	}
}

// listTemplateURIs issues resources/templates/list for the caller ctx
// identifies and returns the visible uriTemplates (nil on an error
// response).
func listTemplateURIs(t *testing.T, s *Server, ctx context.Context) []string {
	t.Helper()
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "resources/templates/list"})
	if resp.Error != nil {
		return nil
	}
	res, ok := resp.Result.(resourceTemplatesListResult)
	if !ok {
		t.Fatalf("resources/templates/list result type %T", resp.Result)
	}
	uris := make([]string, 0, len(res.ResourceTemplates))
	for _, tpl := range res.ResourceTemplates {
		uris = append(uris, tpl.URITemplate)
	}
	return uris
}

// TestTemplatesListWireShape pins the resources/templates/list wire shape
// against the MCP spec (2025-06-18): each template carries uriTemplate,
// name, and optional description/mimeType, and a short set omits
// nextCursor.
func TestTemplatesListWireShape(t *testing.T) {
	s := NewServer()
	mustRegisterTemplate(t, s, "help://docs/{topic}", "Docs",
		WithResourceTemplateDescription("Per-topic docs"))

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/templates/list"})
	if resp.Error != nil {
		t.Fatalf("resources/templates/list errored: %v", resp.Error)
	}
	got := wireJSON(t, resp)
	want := `{"resourceTemplates":[{"uriTemplate":"help://docs/{topic}",` +
		`"name":"Docs","description":"Per-topic docs","mimeType":"text/markdown"}]}`
	if got != want {
		t.Errorf("resources/templates/list wire shape:\n got %s\nwant %s", got, want)
	}
}

func TestRegisterTemplateValidation(t *testing.T) {
	s := NewServer()
	if err := s.RegisterResourceTemplate("", "T", "text/markdown"); err == nil {
		t.Error("empty uriTemplate should error")
	}
	if err := s.RegisterResourceTemplate("ui://x/{id}", "", "text/markdown"); err == nil {
		t.Error("empty name should error")
	}
	if err := s.RegisterResourceTemplate("ui://x/{id}", "T", "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResourceTemplate("ui://x/{id}", "T2", "text/markdown"); err == nil {
		t.Error("duplicate uriTemplate should error")
	}
}

// A templates-only server (no concrete resources) still advertises the
// `resources` capability: the spec has one capability for both surfaces,
// and resources/templates/list is part of it.
func TestTemplatesAdvertiseResources(t *testing.T) {
	s := NewServer()
	mustRegisterTemplate(t, s, "ui://apps/{appId}/manifest", "App manifests")

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize errored: %v", resp.Error)
	}
	if blob := wireJSON(t, resp); !strings.Contains(blob, `"resources"`) {
		t.Errorf("templates-only server did not advertise the resources capability: %s", blob)
	}
}
