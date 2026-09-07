//go:build red

package mcpclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a client read loop makes progress under any protocol-violating byte stream from
// a third-party MCP server peer — a duplicate response for a pending id must never park the
// loop.
// Surfaces: mcpclient/client.go::readLoop (blocking `ch <- r` on a capacity-1 channel; the
// pending entry is deleted only by the Call goroutine's deferred cleanup), ::Call,
// ::Close/failPending.
// Finding: a peer that answers one id N times wedges the loop: dup #1 fills the response
// channel's single buffer slot, dup #2 blocks until the waiting Call receives #1, dup #2's
// element then sits in the buffer, and a further duplicate whose pending-map lookup still
// observes the not-yet-cleaned entry parks forever on the full channel — the scanner never
// advances, every later Call hangs until its context deadline, and Close cannot unblock it.
// One duplicate burst permanently bricks the client against that peer.
// Fix direction: never block the read loop on a caller-owned channel — deliver with a
// non-blocking send (select/default), or bound the per-id buffer, so a duplicate response
// is dropped instead of parking the loop.
//
// The stub answers initialize normally and floods every tools/call with the
// SAME response id five times, then stays silent. Rounds repeat the flooded
// call because the parking window is a scheduling race between the dup's
// pending-map lookup and the Call goroutine's deferred cleanup; several
// bursts in a row make the wedge reproducible per run while each individual
// round stays deterministic protocol-wise (no clocks, no sleeps).

// writeDupFloodStub writes a protocol-violating MCP peer: normal initialize,
// then a 5-copy duplicate-response burst for every tools/call. Shell script
// on Unix, compiled helper on Windows (mirroring stub_unix_test.go /
// stub_windows_test.go).
func writeDupFloodStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		src := filepath.Join(dir, "dupstub.go")
		bin := filepath.Join(dir, "dupstub.exe")
		const source = `package main

import (
  "bufio"
  "encoding/json"
  "fmt"
  "os"
  "strings"
)

func main() {
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
    case "tools/call":
      resp := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"dup\"}]}}\n", id)
      fmt.Print(strings.Repeat(resp, 5))
    }
  }
}
`
		if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
			t.Fatalf("setup broken: build dup stub: %v\n%s", err, out)
		}
		return bin
	}
	path := filepath.Join(dir, "dup-mcp.sh")
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
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}\n' "$id"
      ;;
    *'"method":"tools/call"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      resp='{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"dup"}]}}'
      printf '%s\n%s\n%s\n%s\n%s\n' "$resp" "$resp" "$resp" "$resp" "$resp"
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReadLoopRedDupResponseWedge: after a peer's duplicate-response burst,
// later Calls on fresh ids must still complete and Close must still return.
func TestReadLoopRedDupResponseWedge(t *testing.T) {
	stub := writeDupFloodStub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := Spawn(ctx, stub, nil, "")
	if err != nil {
		t.Fatalf("setup broken: spawn dup stub: %v", err)
	}

	const rounds = 10
	for i := 1; i <= rounds; i++ {
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := c.Call(callCtx, "tools/call", map[string]any{"name": "echo", "arguments": nil})
		callCancel()
		if err != nil {
			t.Errorf("SECURITY: [mcpclient-readloop-wedge] read loop parked after a duplicate-response burst (round %d/%d): %v — one protocol-violating burst from a third-party MCP peer permanently bricks the client; every later Call hangs until its context deadline", i, rounds, err)
			break
		}
	}

	// Close must return even with a (possibly parked) read loop.
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Error("SECURITY: [mcpclient-readloop-wedge] Close() did not return while the read loop was parked; the client cannot even be torn down cleanly")
	}
}
