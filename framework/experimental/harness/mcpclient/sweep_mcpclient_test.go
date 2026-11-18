package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeModeStub(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	path := filepath.Join(t.TempDir(), "mode-mcp.sh")
	script := `#!/bin/sh
mode="$1"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ "$mode" = env ]; then env > "$2"; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}\n' "$id" ;;
    *'"method":"tools/call"'*)
      if [ "$mode" = huge ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":"' "$id"; head -c 5000000 /dev/zero | tr '\0' a; printf '"}\n'
      fi ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnreadableLineFailsPendingCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Spawn(ctx, writeModeStub(t), []string{"huge"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.CallTool(ctx, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "read:") {
		t.Fatalf("Call err = %v, want the read loop's error", err)
	}
}

func TestExplicitEnvWinsOverAllowlist(t *testing.T) {
	t.Setenv("HOME", "/host-home")
	dump := filepath.Join(t.TempDir(), "env.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := SpawnWithConfig(ctx, writeModeStub(t), []string{"env", dump}, "", SpawnConfig{Env: []string{"HOME=/explicit-home"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := readDump(t, dump)
	if !strings.Contains(got, "HOME=/explicit-home") || strings.Contains(got, "/host-home") {
		t.Fatalf("explicit HOME did not win:\n%s", got)
	}
}

func TestMCPToolsAreMutating(t *testing.T) {
	if !(&mcpTool{}).Mutating() {
		t.Fatal("an MCP tool must be treated as mutating")
	}
}
