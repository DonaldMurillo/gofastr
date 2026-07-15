package main

// End-to-end MCP-readiness gate for the blueprint surface: a generated app
// must expose the full agent-facing MCP contract, not just entity CRUD
// tools. The framework is AI-first — an agent pointed at a generated app
// must be able to discover the server (server card), stream it (GET /mcp),
// and orient inside it (the WithMCPIntrospection tool set: app_routes,
// framework_docs_search, …), exactly like examples/site. Gated by -short.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestE2E_MCP_BlueprintApp(t *testing.T) {
	if testing.Short() {
		t.Skip("mcp e2e: compiles and serves a generated app")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), testBlueprintYAML())

	generate := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from=gofastr.yml")
	generate.Dir = dir
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate failed: %v\n%s", err, output)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, output)
	}
	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = dir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	port := nextE2EPort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := exec.CommandContext(ctx, filepath.Join(dir, "app"))
	app.Dir = dir
	app.Env = append(os.Environ(),
		"PORT=localhost:"+port,
		"DATABASE_URL=file:"+filepath.Join(dir, "mcp-e2e.db"),
		// The dev loop's env: the generated app lights up the log MCP
		// debug tools under it (they stay off for untrusted prod /mcp).
		"GOFASTR_DEV=1",
	)
	var appOut syncBuffer
	app.Stdout = &appOut
	app.Stderr = &appOut
	app.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := app.Start(); err != nil {
		t.Fatalf("start generated app: %v", err)
	}
	pid := app.Process.Pid
	t.Cleanup(func() {
		cancel()
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = app.Wait()
	})

	base := "http://localhost:" + port
	waitForBody(t, base+"/", 90*time.Second, &appOut)

	// Contract 1: tools/list serves BOTH the per-entity CRUD tools and the
	// introspection set. An agent must be able to orient (app_routes,
	// framework_docs_*) on the same server it mutates through.
	tools := mcpToolNames(t, base)
	for _, want := range []string{
		"posts_list", "posts_create", // entity CRUD (mcp: true in the blueprint)
		"app_routes", "app_modules", "app_readiness", // app introspection
		"framework_docs_list", "framework_docs_get", "framework_docs_search",
		// The debug loop (battery/log): under GOFASTR_DEV the generated
		// app answers "recent requests / current errors / trace this id".
		"log_recent", "log_filter", "log_metrics", "log_set_level",
		// The control loop (WithMCPControl): runtime state mutation,
		// dev-gated like the log tools.
		"app_module_enable", "app_module_disable",
		// Dev implies entity data tools for EVERY CRUD entity — users has
		// no `mcp: true` in the blueprint, dev serves its tools anyway.
		"users_list", "users_create", "users_update",
	} {
		if !tools[want] {
			t.Errorf("tools/list is missing %q; got %d tools: %v", want, len(tools), sortedKeys(tools))
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Contract 2: the Streamable HTTP transport is complete — GET /mcp
	// answers the SSE stream, not 405.
	req, _ := http.NewRequest(http.MethodGet, base+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	getCtx, getCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer getCancel()
	resp, err := http.DefaultClient.Do(req.WithContext(getCtx))
	if err == nil {
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /mcp: want 200 SSE stream, got %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	} else if !strings.Contains(err.Error(), "context deadline exceeded") {
		// A live SSE stream may outlast the probe window — that's a pass.
		t.Errorf("GET /mcp: %v", err)
	}

	// Contract 3: agent discovery — the MCP server card well-knowns serve.
	for _, path := range []string{
		"/mcp/server-card",
		"/.well-known/mcp/server-card.json",
		"/.well-known/mcp/catalog.json",
	} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: want 200, got %d", path, resp.StatusCode)
		}
	}

	// Contract 4: the introspective tools answer with real data.
	var routes struct {
		Count int `json:"count"`
	}
	mcpCallTool(t, base, "app_routes", &routes)
	if routes.Count == 0 {
		t.Error("app_routes returned zero routes on a serving app")
	}

	// Contract 5: the debug loop closes — the page hits above are already
	// in the ring, so log_recent returns real entries.
	var recent struct {
		Entries []map[string]any `json:"entries"`
	}
	mcpCallTool(t, base, "log_recent", &recent)
	if len(recent.Entries) == 0 {
		t.Error("log_recent returned zero entries on an app that served requests")
	}

	// Contract 6 (fail-closed): WITHOUT the dev env the log tools must not
	// register — access logs carry client IPs and paths, so an untrusted
	// production /mcp never exposes them by default.
	cancel()
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = app.Wait()
	prodPort := nextE2EPort()
	prodCtx, prodCancel := context.WithCancel(context.Background())
	defer prodCancel()
	prod := exec.CommandContext(prodCtx, filepath.Join(dir, "app"))
	prod.Dir = dir
	prod.Env = append(os.Environ(),
		"PORT=localhost:"+prodPort,
		"DATABASE_URL=file:"+filepath.Join(dir, "mcp-e2e.db"),
	)
	var prodOut syncBuffer
	prod.Stdout = &prodOut
	prod.Stderr = &prodOut
	prod.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := prod.Start(); err != nil {
		t.Fatalf("start generated app (prod mode): %v", err)
	}
	prodPid := prod.Process.Pid
	t.Cleanup(func() {
		prodCancel()
		_ = syscall.Kill(-prodPid, syscall.SIGKILL)
		_ = prod.Wait()
	})
	prodBase := "http://localhost:" + prodPort
	waitForBody(t, prodBase+"/", 90*time.Second, &prodOut)
	prodTools := mcpToolNames(t, prodBase)
	for _, banned := range []string{
		"log_recent", "log_filter", "log_metrics", "log_set_level",
		"app_module_enable", "app_module_disable",
		// users has no `mcp: true` — its data tools are dev-implied only.
		"users_list", "users_create",
	} {
		if prodTools[banned] {
			t.Errorf("prod-mode /mcp exposes %q — mutating/debug tools must be dev-gated", banned)
		}
	}
	if !prodTools["posts_list"] {
		t.Error("prod-mode /mcp lost posts_* — explicit `mcp: true` entities must keep their tools outside dev")
	}
	if !prodTools["app_routes"] {
		t.Error("prod-mode /mcp lost the introspection tools — only the log tools should be dev-gated")
	}
}

// mcpToolNames lists the server's tools over real JSON-RPC.
func mcpToolNames(t *testing.T, base string) map[string]bool {
	t.Helper()
	body := mcpPost(t, base, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var r struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("tools/list decode: %v\n%s", err, body)
	}
	out := make(map[string]bool, len(r.Result.Tools))
	for _, tool := range r.Result.Tools {
		out[tool.Name] = true
	}
	return out
}

// mcpCallTool invokes a tool and decodes the text-content payload into out.
func mcpCallTool(t *testing.T, base, name string, out any) {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, name)
	body := mcpPost(t, base, payload)
	var r struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("tools/call %s decode: %v\n%s", name, err, body)
	}
	if r.Error != nil {
		t.Fatalf("tools/call %s: %s", name, r.Error.Message)
	}
	if len(r.Result.Content) == 0 {
		t.Fatalf("tools/call %s: empty content", name)
	}
	if err := json.Unmarshal([]byte(r.Result.Content[0].Text), out); err != nil {
		t.Fatalf("tools/call %s payload decode: %v\n%s", name, err, r.Result.Content[0].Text)
	}
}

func mcpPost(t *testing.T, base, payload string) []byte {
	t.Helper()
	resp, err := http.Post(base+"/mcp", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("POST /mcp read: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp: status %d\n%s", resp.StatusCode, body)
	}
	return body
}
