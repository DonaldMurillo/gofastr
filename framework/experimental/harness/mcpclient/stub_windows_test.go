//go:build windows

package mcpclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeMCPStub(t *testing.T, dumpEnv bool) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "stub.go")
	bin := filepath.Join(dir, "stub-mcp.exe")
	if dumpEnv {
		bin = filepath.Join(dir, "env-dump-mcp.exe")
	}
	const source = `package main

import (
  "bufio"
  "encoding/json"
  "fmt"
  "os"
  "strings"
)

func main() {
  if len(os.Args) > 1 {
    _ = os.WriteFile(os.Args[1], []byte(strings.Join(os.Environ(), "\n")+"\n"), 0644)
  }
  scanner := bufio.NewScanner(os.Stdin)
  for scanner.Scan() {
    var request map[string]json.RawMessage
    if json.Unmarshal(scanner.Bytes(), &request) != nil { continue }
    var method string
    _ = json.Unmarshal(request["method"], &method)
    id := request["id"]
    if len(id) == 0 { id = []byte("null") }
    switch method {
    case "initialize":
      fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{}}}\n", id)
    case "tools/list":
      fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"echo\",\"description\":\"echoes\",\"inputSchema\":{\"type\":\"object\"}}]}}\n", id)
    case "tools/call":
      fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello from stub\"}]}}\n", id)
    }
  }
}
`
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("build MCP stub: %v\n%s", err, out)
	}
	return bin
}
