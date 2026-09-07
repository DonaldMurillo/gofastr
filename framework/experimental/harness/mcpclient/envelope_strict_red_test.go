//go:build red

package mcpclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T2). Tests-only; no fix applied.
//
// Pinned sibling that makes this the contract: every INBOUND-JSON surface in
// the repo refuses duplicate/case-folded keys, and the parity pin is
// core/mcp/stdioenv_security_test.go: "the same envelope bytes the HTTP
// transport refused with 400 were silently executed over stdio" — one wire
// shape, one rule, regardless of transport. Round 5 already treats this
// third-party peer as protocol-violating (readloop_red_test.go: a duplicate
// response must never park the loop). This file covers the decode half of
// the same peer.
//
// Property: the harness's MCP client refuses ambiguous response envelopes
// from the third-party server — a response line carrying a duplicate or
// case-folded "id" key must never resolve the pending call, because stdlib
// json keeps the LAST occurrence while any first-read intermediary sees the
// first, and the harness would execute a tool result whose correlation id
// no two readers agree on.
//
// Surfaces: client.go::readLoop :316-320 — plain json.Unmarshal per server
// line into the response envelope; ::Call resolves whatever id survives the
// last-wins decode. (ListTools :210-216 re-decodes the result blob with the
// same lenient stdlib decode.)
//
// Finding (verified today, legs below): a peer answering Call(id=1) with
// {"jsonrpc":"2.0","id":1,"id":2,"result":{...}} RESOLVES the call — the
// envelope's last "id" wins, the pending entry matches, and the result
// executes; the folded "ID"/"id" spelling resolves the same way. The HTTP
// transport refuses these exact bytes.
//
// Fix direction: strict-decode each server line (handler.UnmarshalStrict or
// the CheckObjectKeys walk) and, on ambiguity, fail the matching pending
// call (or drop the line and let failPending surface the dead peer) — never
// resolve under a last-wins id.

// writeAmbigEnvStub writes a stub MCP peer whose tools/call answer depends
// on mode: "dup" answers with a duplicate id key (last one echoing the
// request's id, so today's last-wins decode resolves), "fold" answers with
// a case-folded ID/id pair, "clean" answers normally. Shell script on Unix,
// compiled helper on Windows (mirroring stub_unix_test.go /
// stub_windows_test.go).
func writeAmbigEnvStub(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		src := filepath.Join(dir, "ambstub.go")
		bin := filepath.Join(dir, "ambstub.exe")
		const source = `package main

import (
  "bufio"
  "encoding/json"
  "fmt"
  "os"
)

func main() {
  mode := "clean"
  if len(os.Args) > 1 { mode = os.Args[1] }
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
      switch mode {
      case "dup":
        fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":1,\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"amb-dup\"}]}}\n", id)
      case "fold":
        fmt.Printf("{\"jsonrpc\":\"2.0\",\"ID\":1,\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"amb-fold\"}]}}\n", id)
      default:
        fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"clean\"}]}}\n", id)
      }
    }
  }
}
`
		if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
			t.Fatalf("setup broken: build ambiguous-envelope stub: %v\n%s", err, out)
		}
		return bin
	}
	path := filepath.Join(dir, "amb-mcp.sh")
	script := `#!/bin/sh
mode="$1"
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{}}}\n' "$id"
      ;;
    *'"method":"notifications/initialized"'*)
      ;;
    *'"method":"tools/call"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
      case "$mode" in
        dup)  printf '{"jsonrpc":"2.0","id":1,"id":%s,"result":{"content":[{"type":"text","text":"amb-dup"}]}}\n' "$id" ;;
        fold) printf '{"jsonrpc":"2.0","ID":1,"id":%s,"result":{"content":[{"type":"text","text":"amb-fold"}]}}\n' "$id" ;;
        *)    printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"clean"}]}}\n' "$id" ;;
      esac
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestResponseRedRefusesAmbiguousEnvelope: the stub answers the first
// post-handshake Call (request id 2) with the bytes from the finding — for
// the dup leg literally {"jsonrpc":"2.0","id":1,"id":2,"result":{...}}.
// Call must ERROR on such an answer; today it resolves.
func TestResponseRedRefusesAmbiguousEnvelope(t *testing.T) {
	for _, leg := range []struct{ name, mode string }{
		{"exact duplicate id keys", "dup"},
		{"case-folded ID/id keys", "fold"},
	} {
		t.Run(leg.name, func(t *testing.T) {
			stub := writeAmbigEnvStub(t, leg.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			c, err := Spawn(ctx, stub, []string{leg.mode}, "")
			if err != nil {
				t.Fatalf("setup broken: spawn %s stub: %v", leg.mode, err)
			}

			callCtx, callCancel := context.WithTimeout(ctx, 3*time.Second)
			res, err := c.Call(callCtx, "tools/call", map[string]any{"name": "echo", "arguments": nil})
			callCancel()
			if err == nil {
				t.Errorf("SECURITY: [mcpclient-envelope-lenient] Call resolved under an ambiguous response envelope (result=%.80s): readLoop's plain json.Unmarshal keeps the LAST duplicate/case-folded id, so the harness executed a peer answer whose correlation id any first-read intermediary parsed differently — core/mcp refuses these exact envelope bytes on every transport it owns (stdioenv pin); strict-decode each server line and fail the pending call instead", string(res))
			}

			// Teardown must stay bounded whatever the loop did.
			closed := make(chan error, 1)
			go func() { closed <- c.Close() }()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Error("SECURITY: [mcpclient-envelope-lenient] Close() did not return after the ambiguous-envelope leg; the client cannot be torn down cleanly")
			}
		})
	}

	// GREEN-guard: a clean envelope resolves.
	stub := writeAmbigEnvStub(t, "clean")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := Spawn(ctx, stub, []string{"clean"}, "")
	if err != nil {
		t.Fatalf("setup broken: spawn clean stub: %v", err)
	}
	res, err := c.Call(ctx, "tools/call", map[string]any{"name": "echo", "arguments": nil})
	if err != nil || !strings.Contains(string(res), "clean") {
		t.Fatalf("setup broken: clean response must resolve (err=%v res=%.80s)", err, string(res))
	}
	_ = c.Close()
}
