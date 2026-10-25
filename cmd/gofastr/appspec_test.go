package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestGenerateFromAppSpecDryRunJSONReportsValidationErrors(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "bad.yml"), `
endpoints:
  - name: bad
    entity: ghosts
    method: POST
    path: "{id}/bad"
    handler: entities/ghosts_endpoints.go#Bad
`)
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "app", filepath.Join(dir, "bad.yml"), "--dry-run", "--json")
	cmd.Dir = repoRoot
	// Inherit the parent test's GOCACHE; overriding with a fresh
	// cache here races against the outer `go test ./...` parallel
	// build of the same module and transiently mis-resolves the 10
	// battery imports in cmd/gofastr/internal/agentscmd/agents.go during vet's
	// type-check phase. The fresh cache wasn't load-bearing for
	// what this test asserts (dry-run JSON error shape).
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for invalid app spec\n%s", stdout.String())
	}
	var got struct {
		Files  []any `json:"files"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("dry-run JSON errors did not parse: %v\nstdout:\n%s\nstderr:\n%s", jsonErr, stdout.String(), stderr.String())
	}
	if len(got.Files) != 0 || len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, `endpoint bad hangs under entity "ghosts"`) {
		t.Fatalf("unexpected dry-run JSON error payload: %#v", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "gen")); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run with validation errors wrote output dir: %v", statErr)
	}
}

func TestGenerateFromAppSpecDryRunJSONReportsUnsafeOutputPath(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.yml"), `
app:
  name: Demo
`)
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "app", filepath.Join(dir, "gofastr.yml"), "--out=..", "--dry-run", "--json")
	cmd.Dir = repoRoot
	// Inherit parent GOCACHE. See the matching note in
	// TestGenerateFromAppSpecDryRunJSONReportsValidationErrors.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for unsafe output path\n%s", stdout.String())
	}
	var got struct {
		Files  []any `json:"files"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("dry-run JSON errors did not parse: %v\nstdout:\n%s\nstderr:\n%s", jsonErr, stdout.String(), stderr.String())
	}
	if len(got.Files) != 0 || len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, "would target the working directory") {
		t.Fatalf("unexpected dry-run JSON error payload: %#v", got)
	}
}

func TestAppSpecCLIGeneratesEntireWorkingAppE2E(t *testing.T) {
	clitest.VariantBuild(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.yml"), clitest.TestAppSpecYAML())
	if err := os.Mkdir(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "public", "hello.txt"), "static from generated app")

	runGoFastr := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "app", "gofastr.yml")
	runGoFastr.Dir = dir
	if output, err := runGoFastr.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Fatalf("generated app entrypoint missing: %v", err)
	}

	appBin := clitest.TestExecutablePath(filepath.Join(dir, "generated-app-spec-app"))
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	buildCmd.Dir = dir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build generated app failed: %v\n%s", err, output)
	}

	addr := clitest.FreeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(dir, "app-spec-e2e.db"),
	)
	// syncBuffer: os/exec copies the child's output from its own goroutines
	// until Wait returns, so reading a plain bytes.Buffer while the app runs
	// races the copier.
	var output clitest.SyncBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated app: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	baseURL := "http://" + addr
	clitest.WaitForHTTP(t, baseURL+"/", &output)
	checkBodyContains(t, baseURL+"/", http.StatusOK, "Generated from YAML.")
	checkBodyContains(t, baseURL+"/", http.StatusOK, `data-island="live_status"`)
	checkBodyContains(t, baseURL+"/", http.StatusOK, `data-widget="save_button"`)
	checkBodyContains(t, baseURL+"/", http.StatusOK, "details-section")
	checkBodyContains(t, baseURL+"/hello.txt", http.StatusOK, "static from generated app")

	// Installable-PWA surface (the fixture enables app.pwa).
	checkBodyContains(t, baseURL+"/manifest.webmanifest", http.StatusOK, `"name": "Demo"`)
	checkBodyContains(t, baseURL+"/service-worker.js", http.StatusOK, "gofastr-pwa-")
	checkBodyContains(t, baseURL+"/icons/icon-192.png", http.StatusOK, "PNG")
	checkBodyContains(t, baseURL+"/icons/icon-512.png", http.StatusOK, "PNG")
	checkBodyContains(t, baseURL+"/icons/icon-maskable.png", http.StatusOK, "PNG")
	checkBodyContains(t, baseURL+"/__gofastr/pwa/offline", http.StatusOK, "offline")
	checkBodyContains(t, baseURL+"/", http.StatusOK, `<link rel="manifest" href="/manifest.webmanifest">`)

	// LLM markdown (the fixture enables app.llm_md): the index lists the
	// screens and every screen llm.md link it emits resolves. The /api/llm.md
	// prose pointer is skipped; it belongs to the entity API surface, not
	// the screen inventory.
	idxResp, err := http.Get(baseURL + "/llm-pages.md")
	if err != nil {
		t.Fatal(err)
	}
	idxBody, _ := io.ReadAll(idxResp.Body)
	idxResp.Body.Close()
	if idxResp.StatusCode != http.StatusOK {
		t.Fatalf("/llm-pages.md status = %d", idxResp.StatusCode)
	}
	if !strings.Contains(string(idxBody), "[/](") {
		t.Errorf("/llm-pages.md should list the root screen:\n%s", idxBody)
	}
	llmLink := regexp.MustCompile(`\((/[^)]*llm\.md)\)`)
	seenScreenLink := false
	for _, m := range llmLink.FindAllStringSubmatch(string(idxBody), -1) {
		if m[1] == "/api/llm.md" {
			continue
		}
		seenScreenLink = true
		linkResp, err := http.Get(baseURL + m[1])
		if err != nil {
			t.Fatal(err)
		}
		linkResp.Body.Close()
		if linkResp.StatusCode != http.StatusOK {
			t.Errorf("llm-pages.md link %s = %d, want 200", m[1], linkResp.StatusCode)
		}
	}
	if !seenScreenLink {
		t.Errorf("/llm-pages.md emitted no screen links:\n%s", idxBody)
	}

	// CRUD routes mount under the default api_prefix ("api") so root paths
	// stay free for the generated HTML screens.
	created := requestCRUDJSON(t, http.MethodPost, baseURL+"/api/posts", map[string]any{"title": "HTTP Post", "status": "draft"}, http.StatusCreated)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("created id = %#v", created["id"])
	}
	got := requestCRUDJSON(t, http.MethodGet, baseURL+"/api/posts/"+id, nil, http.StatusOK)
	if got["title"] != "HTTP Post" {
		t.Fatalf("get title = %#v", got["title"])
	}
	updated := requestCRUDJSON(t, http.MethodPut, baseURL+"/api/posts/"+id, map[string]any{"title": "HTTP Post Updated", "status": "published"}, http.StatusOK)
	if updated["status"] != "published" {
		t.Fatalf("updated status = %#v", updated["status"])
	}
	patched := requestCRUDJSON(t, http.MethodPatch, baseURL+"/api/posts/"+id, map[string]any{"title": "HTTP Post Updated"}, http.StatusOK)
	if patched["status"] != "published" {
		t.Fatalf("patch lost omitted status = %#v", patched["status"])
	}
	list := requestJSON(t, http.MethodGet, baseURL+"/api/posts?limit=10", nil, http.StatusOK)
	data, ok := list["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("list data = %#v", list["data"])
	}
	runBrowserUIE2E(t, baseURL, "HTTP Post Updated")
	_ = requestJSON(t, http.MethodDelete, baseURL+"/api/posts/"+id, nil, http.StatusNoContent)
	resp404, err := http.Get(baseURL + "/api/posts/" + id)
	if err != nil {
		t.Fatal(err)
	}
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted get status = %d", resp404.StatusCode)
	}

	// /openapi.json is auth-gated by default (security hardening).
	// An app spec app that doesn't wire auth gets 401 from an anonymous
	// fetch; that's the expected contract. The path-shape assertion
	// the test really cares about (posts routes appearing in the spec)
	// is covered by `framework/openapi_e2e_test.go`'s spec-handler
	// tests, which authenticate via context.
	resp, err := http.Get(baseURL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /openapi.json status = %d, want 401 (auth-gated by default)", resp.StatusCode)
	}

	// The endpoint's generated stub answers 501 until it is written.
	resp, err = http.Post(baseURL+"/api/posts/"+id+"/publish", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("publish status = %d", resp.StatusCode)
	}

	tools := requestMCP(t, baseURL+"/mcp", "tools/list", nil)
	toolNames := map[string]bool{}
	if result, ok := tools["result"].(map[string]any); ok {
		if rawTools, ok := result["tools"].([]any); ok {
			for _, rawTool := range rawTools {
				if tool, ok := rawTool.(map[string]any); ok {
					if name, ok := tool["name"].(string); ok {
						toolNames[name] = true
					}
				}
			}
		}
	}
	for _, name := range []string{"posts_list", "posts_get", "posts_create", "posts_update", "posts_delete"} {
		if !toolNames[name] {
			t.Fatalf("missing MCP tool %s in %#v", name, toolNames)
		}
	}
	mcpCreated := callMCPHTTP(t, baseURL+"/mcp", "posts_create", map[string]any{"title": "MCP Post", "status": "draft"})
	mcpID, ok := mcpCreated["id"].(string)
	if !ok || mcpID == "" {
		t.Fatalf("mcp create id = %#v", mcpCreated)
	}
	mcpGot := callMCPHTTP(t, baseURL+"/mcp", "posts_get", map[string]any{"id": mcpID})
	if mcpGot["title"] != "MCP Post" {
		t.Fatalf("mcp get = %#v", mcpGot)
	}
	mcpUpdated := callMCPHTTP(t, baseURL+"/mcp", "posts_update", map[string]any{"id": mcpID, "title": "MCP Post", "status": "published"})
	if mcpUpdated["status"] != "published" {
		t.Fatalf("mcp update = %#v", mcpUpdated)
	}
	mcpList := callMCPHTTP(t, baseURL+"/mcp", "posts_list", map[string]any{"limit": 10})
	if rows, ok := mcpList["data"].([]any); !ok || len(rows) != 1 {
		t.Fatalf("mcp list = %#v", mcpList)
	}
	mcpDeleted := callMCPHTTP(t, baseURL+"/mcp", "posts_delete", map[string]any{"id": mcpID})
	if mcpDeleted["deleted"] != true {
		t.Fatalf("mcp delete = %#v", mcpDeleted)
	}
}

func checkBodyContains(t *testing.T, url string, wantStatus int, want string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s status = %d, want %d\n%s", url, resp.StatusCode, wantStatus, raw)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s body missing %q:\n%s", url, want, raw)
	}
}

func runBrowserUIE2E(t *testing.T, baseURL, wantEntityTitle string) {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))

	var hasRuntime, hasActions, hasIsland, hasWidget bool
	var before, after, clicked string
	var backgroundToken, primaryToken, textToken string
	var entityListBody string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(baseURL+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`!!window.__gofastr`, &hasRuntime),
		chromedp.Evaluate(`!!(window.__gofastr && window.__gofastr.handlers && window.__gofastr.handlers["screen-home"])`, &hasActions),
		chromedp.Evaluate(`!!document.querySelector('[data-island="live_status"]')`, &hasIsland),
		chromedp.Evaluate(`!!document.querySelector('[data-widget="save_button"]')`, &hasWidget),
		// The entity_list is server-rendered into a ui.DataTable on first paint
		// (no client fetch / refresh button); the CRUD rows are already in the DOM.
		chromedp.WaitVisible(`[data-cui-comp="ui-data-table"]`, chromedp.ByQuery),
		chromedp.Text(`[data-action-result]`, &before, chromedp.ByQuery),
		chromedp.Click(`#save-action`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Text(`[data-action-result]`, &after, chromedp.ByQuery),
		chromedp.Evaluate(`document.body.getAttribute('data-appspec-clicked') || ''`, &clicked),
		chromedp.Text(`[data-cui-comp="ui-data-table"]`, &entityListBody, chromedp.ByQuery),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--color-background').trim()`, &backgroundToken),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--color-primary').trim()`, &primaryToken),
		chromedp.Evaluate(`getComputedStyle(document.documentElement).getPropertyValue('--color-text').trim()`, &textToken),
	); err != nil {
		t.Fatalf("browser UI e2e failed: %v", err)
	}
	if !hasRuntime || !hasActions || !hasIsland || !hasWidget {
		t.Fatalf("browser state runtime=%t actions=%t island=%t widget=%t", hasRuntime, hasActions, hasIsland, hasWidget)
	}
	if before != "Waiting" || after != "Saved by browser" || clicked != "yes" {
		t.Fatalf("browser action before=%q after=%q clicked=%q", before, after, clicked)
	}
	// The resource formatter humanizes enum values, so "published" renders "Published".
	if !strings.Contains(entityListBody, wantEntityTitle) || !strings.Contains(entityListBody, "Published") {
		t.Fatalf("entity list body missing generated CRUD data: %q", entityListBody)
	}
	if backgroundToken != "#101820" || primaryToken != "#F2AA4C" || textToken != "#F7F4EA" {
		t.Fatalf("computed theme tokens background=%q primary=%q text=%q", backgroundToken, primaryToken, textToken)
	}
}

func requestJSON(t *testing.T, method, url string, body any, wantStatus int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d\n%s", method, url, resp.StatusCode, wantStatus, raw)
	}
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response: %v\n%s", err, raw)
	}
	return out
}

func requestCRUDJSON(t *testing.T, method, url string, body any, wantStatus int) map[string]any {
	t.Helper()
	response := requestJSON(t, method, url, body, wantStatus)
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("CRUD response is not wrapped: %#v", response)
	}
	return data
}

func requestMCP(t *testing.T, url, method string, params map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	return requestJSON(t, http.MethodPost, url, body, http.StatusOK)
}

func callMCPHTTP(t *testing.T, url, name string, params map[string]any) map[string]any {
	t.Helper()
	resp := requestMCP(t, url, "tools/call", map[string]any{"name": name, "arguments": params})
	if errObj := resp["error"]; errObj != nil {
		t.Fatalf("mcp %s failed: %#v", name, errObj)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("mcp %s result = %#v", name, resp["result"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("mcp %s content = %#v", name, result["content"])
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("mcp %s content[0] = %#v", name, content[0])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("mcp %s text = %#v", name, first["text"])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode mcp result: %v\n%s", err, text)
	}
	return out
}
