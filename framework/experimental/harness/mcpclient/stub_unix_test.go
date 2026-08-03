//go:build !windows

package mcpclient

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMCPStub(t *testing.T, dumpEnv bool) string {
	t.Helper()
	dir := t.TempDir()
	name := "stub-mcp.sh"
	if dumpEnv {
		name = "env-dump-mcp.sh"
	}
	path := filepath.Join(dir, name)
	script := `#!/bin/sh
if [ "$1" != "" ]; then env > "$1"; fi
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
