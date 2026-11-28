package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildKiln(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kiln")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build kiln: %v\n%s", err, out)
	}
	return bin
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestServeRunsBehindTheGuard(t *testing.T) {
	bin := buildKiln(t)
	port := freeLoopbackPort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.Command(bin, "serve", "--addr", addr, "--journal", ":memory:", "--agent", "none")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+cmd.Dir, "USERPROFILE="+cmd.Dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	base := "http://" + addr
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := client.Get(base + "/kiln/world")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("kiln serve never answered: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	rebound, _ := http.NewRequest(http.MethodGet, base+"/kiln/world", nil)
	rebound.Host = "attacker.example:" + strconv.Itoa(port)
	if code, body := do(t, client, rebound); code != http.StatusForbidden || !strings.Contains(body, "DNS-rebinding") {
		t.Errorf("GET with a rebound Host: %d %q, want 403 from the Host pin", code, body)
	}

	cross, _ := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(`{}`))
	cross.Header.Set("Origin", "http://attacker.example")
	if code, body := do(t, client, cross); code != http.StatusForbidden || !strings.Contains(body, "cross-origin") {
		t.Errorf("cross-origin POST: %d %q, want 403 from the Origin check", code, body)
	}
}

func do(t *testing.T, client *http.Client, req *http.Request) (int, string) {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestStdioModesWriteOnlyFrames(t *testing.T) {
	bin := buildKiln(t)
	dir := t.TempDir()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n"
	for _, mode := range []struct{ name, stdin string }{{"mcp", initialize}, {"acp", ""}} {
		cmd := exec.Command(bin, mode.name, "--no-http", "--journal", ":memory:")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+dir, "USERPROFILE="+dir)
		cmd.Stdin = strings.NewReader(mode.stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := runWithin(cmd, 30*time.Second); err != nil {
			t.Fatalf("%s: %v\n%s", mode.name, err, stderr.String())
		}
		if stderr.Len() == 0 {
			t.Errorf("%s: nothing on stderr; the startup log is expected there", mode.name)
		}
		for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
			if line == "" {
				continue
			}
			var frame struct {
				JSONRPC string `json:"jsonrpc"`
			}
			if err := json.Unmarshal([]byte(line), &frame); err != nil || frame.JSONRPC != "2.0" {
				t.Errorf("%s: stdout line is not a JSON-RPC frame: %q", mode.name, line)
			}
		}
	}
}

func runWithin(cmd *exec.Cmd, limit time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-done
		return os.ErrDeadlineExceeded
	}
}

func TestRunAgentStartsServeWithServeArgs(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var runAgentFn *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "runAgent" {
			runAgentFn = fn
		}
	}
	if runAgentFn == nil {
		t.Fatal("runAgent not found in agent.go")
	}
	commands, viaServeArgs := 0, 0
	ast.Inspect(runAgentFn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Command" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" || len(call.Args) != 2 || call.Ellipsis == token.NoPos {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); !ok || id.Name != "exe" {
			return true
		}
		commands++
		if inner, ok := call.Args[1].(*ast.CallExpr); ok {
			if fn, ok := inner.Fun.(*ast.Ident); ok && fn.Name == "serveArgs" {
				viaServeArgs++
			}
		}
		return true
	})
	if commands != 1 || viaServeArgs != 1 {
		t.Fatalf("runAgent starts %d serve child(ren), %d through serveArgs; want exactly one, built by serveArgs", commands, viaServeArgs)
	}
}
