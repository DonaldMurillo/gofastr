package mcp

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The stream's opening endpoint event names the path the stream was
// mounted at, the one POSTs go to. It said "/sse" whatever the mount,
// so a client that follows the event (the HTTP+SSE convention it comes
// from) posted to a path nothing serves: the framework mounts /mcp.
func TestEndpointEventNamesMountPath(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(s.ServeSSE("/mcp"))
	t.Cleanup(ts.Close)
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	br := bufio.NewReader(resp.Body)
	var lines []string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before the endpoint event: %v (read %q)", err, lines)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 || lines[0] != "event: endpoint" || lines[1] != "data: /mcp" {
		t.Fatalf("first event %q, want [event: endpoint, data: /mcp]", lines)
	}
}
