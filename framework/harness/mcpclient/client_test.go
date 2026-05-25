package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeStubServer creates a tiny shell-script MCP server that
// answers initialize + tools/list + tools/call for one tool.
func writeStubServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stub-mcp.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}\n' "$id"
      ;;
    *'"method":"notifications/initialized"'*)
      ;;
    *'"method":"tools/list"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"echo","description":"echoes","inputSchema":{"type":"object"}}]}}\n' "$id"
      ;;
    *'"method":"tools/call"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"hello from stub"}]}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSpawnAndListTools(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestCallTool(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hello from stub") {
		t.Errorf("result = %s", string(out))
	}
}

func TestSHA256Mismatch(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Spawn(ctx, server, nil, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected sha256 mismatch")
	}
	if _, ok := err.(*SHA256MismatchError); !ok {
		t.Errorf("err type = %T", err)
	}
}

func TestSourceWraps(t *testing.T) {
	server := writeStubServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Spawn(ctx, server, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	src := NewSource("stub", "eager", c)
	tools, err := src.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name() != "stub.echo" {
		t.Errorf("name = %q", tools[0].Name())
	}
}
