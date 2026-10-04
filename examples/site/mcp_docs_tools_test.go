package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The deployed site's public /mcp advertises the framework docs tools:
// main.go passes framework.WithMCPTools(mcptools.Register), and this
// drives the wire shape (initialize, then tools/list over the stateless
// Streamable HTTP transport) rather than the server's tool table, so a
// dropped option or a registrar that fails past boot both show here.
func TestPublicMCPListsFrameworkDocsTools(t *testing.T) {
	// /mcp mounts in Start, not in setupServer, so boot the app the way
	// the binary does and wait for the bound address.
	fwApp := newTestApp(t)
	ready := make(chan string, 1)
	fwApp.OnReady(func(addr string) { ready <- addr })
	startErr := make(chan error, 1)
	go func() { startErr <- fwApp.Start("127.0.0.1:0") }()
	var base string
	select {
	case addr := <-ready:
		base = "http://" + addr
	case err := <-startErr:
		t.Fatalf("site exited before it was ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("site did not start")
	}
	post := func(body string) string {
		resp, err := http.Post(base+"/mcp", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /mcp: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /mcp = %d: %s", resp.StatusCode, b)
		}
		return string(b)
	}
	if got := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"site-test","version":"0"}}}`); !strings.Contains(got, `"result"`) {
		t.Fatalf("initialize: %s", got)
	}
	got := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	for _, want := range []string{`"framework_docs_list"`, `"framework_docs_get"`, `"framework_docs_search"`, `"app_routes"`} {
		if !strings.Contains(got, want) {
			t.Errorf("tools/list lacks %s: %s", want, got)
		}
	}
}
