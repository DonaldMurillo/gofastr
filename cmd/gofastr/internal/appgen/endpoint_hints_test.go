package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestRenderedEndpointsCarryMCPHints(t *testing.T) {
	ep := func(name, method string, mcp bool, hints settings.EndpointHints) AppEndpoint {
		return AppEndpoint{
			Endpoint: settings.Endpoint{Name: name, Entity: "posts", Method: method, Path: name, Handler: "entities/posts.go#" + strings.ToUpper(name[:1]) + name[1:], MCP: mcp, Hints: hints},
			input:    "struct{}", output: "struct{}",
		}
	}
	src, err := renderAppEndpoints([]AppEndpoint{
		ep("lookup", "GET", true, settings.EndpointHints{}),
		ep("clear", "DELETE", true, settings.EndpointHints{Title: "Clear the cache", Destructive: settings.HintFalse}),
		ep("plain", "POST", false, settings.EndpointHints{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, `"github.com/DonaldMurillo/gofastr/core/mcp"`) {
		t.Fatalf("an MCP endpoint's file does not import core/mcp:\n%s", src)
	}
	compact := strings.Join(strings.Fields(src), " ")
	for name, want := range map[string]string{
		"lookup": `MCPAnnotations: &mcp.Annotations{ ReadOnlyHint: mcp.Bool(true), DestructiveHint: mcp.Bool(false), IdempotentHint: mcp.Bool(true), OpenWorldHint: mcp.Bool(false), }`,
		"clear":  `MCPAnnotations: &mcp.Annotations{ Title: "Clear the cache", ReadOnlyHint: mcp.Bool(false), DestructiveHint: mcp.Bool(false), IdempotentHint: mcp.Bool(true), OpenWorldHint: mcp.Bool(false), }`,
	} {
		block := compact[strings.Index(compact, `Name: "`+name+`"`):]
		block = block[:strings.Index(block, "})")]
		if !strings.Contains(block, want) {
			t.Errorf("%s: generated registration lacks %s:\n%s", name, want, block)
		}
	}
	plain := compact[strings.Index(compact, `Name: "plain"`):]
	if strings.Contains(plain, "MCPAnnotations") {
		t.Errorf("an endpoint without mcp: true got annotations:\n%s", plain)
	}

	bare, err := renderAppEndpoints([]AppEndpoint{ep("plain", "POST", false, settings.EndpointHints{})})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bare, "core/mcp") {
		t.Fatalf("a file with no MCP endpoint imports core/mcp:\n%s", bare)
	}
}
