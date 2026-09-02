package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// Property: resources/read resolves a uri by exact registry key only.
// There is no filesystem access, no path normalization, no
// percent-decoding and no case folding between the caller's uri and the
// registered one — so a uri shaped like a path traversal, a file URL, or
// an encoded variant of a real uri is simply an unknown key, never a
// read of a different resource and never a not-found oracle that
// normalizes two spellings into one.
func TestResourcesReadURINeverNormalizes(t *testing.T) {
	s := NewServer()
	if err := staticResource(s, "ui://app/x.html", "X", "text/html", "<h1>contents</h1>"); err != nil {
		t.Fatal(err)
	}

	shapes := []string{
		"file:///etc/passwd",
		"/etc/passwd",
		"../../etc/passwd",
		"ui://app/../../etc/passwd",
		"ui://app/x/../x.html",
		"ui://app/%2e%2e/x.html",
		"ui://app/%2E%2E/x.html",
		"ui://app/%78.html", // percent-encoded 'x'
		"ui://APP/X.HTML",   // case-folded
		"ui://app/x.html/",  // trailing slash
		"ui://app/x.html ",  // trailing space
		" ui://app/x.html",  // leading space
		"ui://app/x.html\x00",
		"ui://app/./x.html",
		"ui://app//x.html",
	}
	for _, uri := range shapes {
		p, _ := json.Marshal(map[string]any{"uri": uri})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p,
		})
		if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
			t.Errorf("SECURITY: [traversal] uri %q resolved to a read (resp=%+v); resources/read must be an exact map-key lookup with no path, percent-decoding or case normalization",
				uri, resp)
		}
	}

	// The exact key — byte for byte — still reads. The refusal above must
	// come from the lookup, not from the read path being closed.
	p, _ := json.Marshal(map[string]any{"uri": "ui://app/x.html"})
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: p,
	})
	if resp.Error != nil {
		t.Fatalf("exact registered uri failed to read: %v", resp.Error)
	}
}
