package main

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func readmeContent(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(clitest.RepoRootDir(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return string(raw)
}

func TestReadmeAnchorMissingFails(t *testing.T) {
	if _, err := clitest.ReadmeSection("# Other\n\nNo quickstart here.\n", clitest.ReadmeAppSpecHeading); err == nil {
		t.Fatal("missing heading did not error")
	}
	if _, err := clitest.FencedBlock("### Declare an app (app spec)\n\n```yaml\nfoo: bar\n```\n", "yaml", "# gofastr.yml"); err == nil {
		t.Fatal("missing anchored fence did not error")
	}
}

func TestReadmeQuickstartShapeIsStable(t *testing.T) {
	yamlBlock, bashBlock, initBlock := clitest.ReadmeQuickstartBlocks(t)
	for _, want := range []string{"entities:", "screens:"} {
		if !strings.Contains(yamlBlock, want) {
			t.Fatalf("tutorial app spec block lost %q — the quickstart yaml must keep both halves (entities AND screens):\n%s", want, yamlBlock)
		}
	}
	// `gofastr dev`, not `go run .`: only the dev server hot-reloads, and the
	// quickstart is the development on-ramp. TestReadmeQuickstartAppSpecRuns
	// keeps the sequence executable by building and booting the same app.
	for _, want := range []string{"gofastr validate", "gofastr generate app gofastr.yml", "go mod tidy", "gofastr dev"} {
		if !strings.Contains(bashBlock, want) {
			t.Fatalf("tutorial quickstart command sequence lost %q — the documented path must stay the executable path:\n%s", want, bashBlock)
		}
	}
	if !strings.Contains(initBlock, "go mod init") {
		t.Fatalf("tutorial quickstart lost its `go mod init` step:\n%s", initBlock)
	}
}

// The Quickstart shows one complete program per layer, each under its
// own pinned heading. All three go through the same executable gate.
const (
	readmeCoreHeading       = "### Core only"
	readmeEntityHeading     = "### Framework"
	readmeDonaldsWayHeading = "### Full stack"
)

// readmeProgram extracts the complete Go program (the first ```go fence
// with a func main) under the given pinned heading.
func readmeProgram(t *testing.T, heading string) string {
	t.Helper()
	section, err := clitest.ReadmeSection(readmeContent(t), heading)
	if err != nil {
		t.Fatal(err)
	}
	block, err := clitest.FencedBlock(section, "go", "func main()")
	if err != nil {
		t.Fatalf("README program under %q missing: %v", heading, err)
	}
	return block
}

// buildAndBootReadmeProgram compiles an extracted README program against
// the working tree (the one transform: the hard-coded :8080 becomes a
// free port), boots it, and waits until readyPath responds. The process
// is killed via t.Cleanup.
func buildAndBootReadmeProgram(t *testing.T, src, module, binName, readyPath string) (string, *clitest.SyncBuffer) {
	t.Helper()
	repoRoot := clitest.RepoRootDir(t)
	dir := t.TempDir()
	addr := clitest.FreeAddr(t)
	src = strings.Replace(src, `":8080"`, `"`+addr+`"`, 1)

	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module " + module + "\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), src)

	appBin := clitest.TestExecutablePath(filepath.Join(dir, binName))
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	buildCmd.Dir = dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("README %s program did not build: %v\n%s", binName, err, out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	// syncBuffer: os/exec copies the child's output from its own goroutines
	// until Wait returns, so reading a plain bytes.Buffer while the app runs
	// races the copier.
	output := &clitest.SyncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start README %s: %v", binName, err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	baseURL := "http://" + addr
	clitest.WaitForHTTP(t, baseURL+readyPath, output)
	return baseURL, output
}

// TestReadmeCoreQuickstartRuns gates the "Core only" program: the plain
// core/router + core/render + core/handler app must serve its HTML page
// and its typed JSON route exactly as the README shows.
func TestReadmeCoreQuickstartRuns(t *testing.T) {
	src := readmeProgram(t, readmeCoreHeading)
	baseURL, output := buildAndBootReadmeProgram(t, src, "readme.example/core", "readme-core-app", "/")

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Hello from core") {
		t.Fatalf("GET / = %d, body %q — want 200 with the rendered heading\n%s", resp.StatusCode, body, output.String())
	}

	pingResp, err := http.Get(baseURL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	pingBody, _ := io.ReadAll(pingResp.Body)
	pingResp.Body.Close()
	if pingResp.StatusCode != http.StatusOK || !strings.Contains(string(pingBody), `"status":"ok"`) {
		t.Fatalf("GET /api/ping = %d, body %q — want the typed JSON response\n%s", pingResp.StatusCode, pingBody, output.String())
	}
}

// TestReadmeDonaldsWayQuickstartRuns gates the full-app program: the
// server-rendered screen serves HTML at /, the OwnerField entity
// answers anonymous API reads with 401, the auth battery's routes are
// mounted (login answers 400 on an empty body, NOT 404), and the MCP
// endpoint lists the entity tools, the four claims the section makes.
func TestReadmeDonaldsWayQuickstartRuns(t *testing.T) {
	src := readmeProgram(t, readmeDonaldsWayHeading)
	if !strings.Contains(src, "OwnerField") {
		t.Fatalf("README Donald's Way program lost OwnerField — it backs the anonymous-401 claim:\n%s", src)
	}
	// The Init error must stay checked: an unchecked Init boots a server
	// whose auth routes silently never mounted (the bug this replaced).
	if !strings.Contains(src, "if err := authMgr.Init(fwApp); err != nil") {
		t.Fatalf("README Donald's Way program no longer checks authMgr.Init's error — auth fails silently without it:\n%s", src)
	}
	baseURL, output := buildAndBootReadmeProgram(t, src, "readme.example/donaldsway", "readme-donaldsway-app", "/")

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "My notes") {
		t.Fatalf("GET / = %d — want 200 with the server-rendered screen (body %q)\n%s", resp.StatusCode, body, output.String())
	}

	llmResp, err := http.Get(baseURL + "/llm.md")
	if err != nil {
		t.Fatal(err)
	}
	llmBody, _ := io.ReadAll(llmResp.Body)
	llmResp.Body.Close()
	if llmResp.StatusCode != http.StatusOK || !strings.Contains(string(llmBody), "My notes") {
		t.Fatalf("GET /llm.md = %d — want 200 with page markdown (body %q)\n%s", llmResp.StatusCode, llmBody, output.String())
	}

	apiResp, err := http.Get(baseURL + "/api/notes")
	if err != nil {
		t.Fatal(err)
	}
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/notes = %d, want 401 (OwnerField is secure by default)\n%s", apiResp.StatusCode, output.String())
	}

	loginResp, err := http.Post(baseURL+"/auth/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode == http.StatusNotFound {
		t.Fatalf("POST /auth/login = 404 — the auth battery's routes did not mount\n%s", output.String())
	}

	listReq := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	mcpResp, err := http.Post(baseURL+"/mcp", "application/json", strings.NewReader(listReq))
	if err != nil {
		t.Fatalf("POST /mcp tools/list: %v\n%s", err, output.String())
	}
	mcpBody, _ := io.ReadAll(mcpResp.Body)
	mcpResp.Body.Close()
	if !strings.Contains(string(mcpBody), "notes_list") {
		t.Fatalf("POST /mcp tools/list missing notes_list (got: %s)\n%s", mcpBody, output.String())
	}
}

// TestReadmeGoQuickstartRuns is the executable gate for the Framework
// snippet. It extracts the exact program, swaps only the hard-coded listen
// address for a free port, compiles it against the working tree, boots it,
// and asserts the runtime claims the section makes:
//   - GET / == 200 with the framework/ui Card heading ("Ship faster")
//   - GET /api/hello == 200 with the typed JSON payload ("hello from go")
//   - POST /mcp initialize returns a JSON-RPC result, tools/list contains
//     "greet", and tools/call "greet" returns "hello from mcp".
func TestReadmeGoQuickstartRuns(t *testing.T) {
	src := readmeProgram(t, readmeEntityHeading)
	for _, want := range []string{"framework.WithMCP()", "framework.WithMCPTools", "ui.Card"} {
		if !strings.Contains(src, want) {
			t.Fatalf("README framework snippet lost %q — it backs a runtime claim:\n%s", want, src)
		}
	}

	baseURL, output := buildAndBootReadmeProgram(t, src, "readme.example/framework", "readme-framework-app", "/")

	getResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK || !strings.Contains(string(pageBody), "Ship faster") {
		t.Fatalf("GET / = %d, body %q — want 200 with the rendered Card heading\n%s", getResp.StatusCode, pageBody, output.String())
	}

	apiResp, err := http.Get(baseURL + "/api/hello")
	if err != nil {
		t.Fatalf("GET /api/hello: %v\n%s", err, output.String())
	}
	apiBody, _ := io.ReadAll(apiResp.Body)
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK || !strings.Contains(string(apiBody), `"message":"hello from go"`) {
		t.Fatalf("GET /api/hello = %d, body %q — want typed JSON greeting\n%s", apiResp.StatusCode, apiBody, output.String())
	}

	client := &http.Client{Timeout: 5 * time.Second}
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"readme-gate","version":"0"}}}`
	initResp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(initReq))
	if err != nil {
		t.Fatalf("POST /mcp initialize: %v\n%s", err, output.String())
	}
	initBody, _ := io.ReadAll(initResp.Body)
	initResp.Body.Close()
	if initResp.StatusCode == http.StatusNotFound {
		t.Fatalf("POST /mcp = 404 — WithMCP() did not mount /mcp\n%s", output.String())
	}
	if !strings.Contains(string(initBody), `"result"`) {
		t.Fatalf("POST /mcp initialize did not return a JSON-RPC result (status %d): %s\n%s", initResp.StatusCode, initBody, output.String())
	}
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	listResp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(listReq))
	if err != nil {
		t.Fatalf("POST /mcp tools/list: %v\n%s", err, output.String())
	}
	listBody, _ := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if !strings.Contains(string(listBody), `"greet"`) {
		t.Fatalf("POST /mcp tools/list missing greet (got: %s)\n%s", listBody, output.String())
	}
	callReq := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet","arguments":{}}}`
	callResp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(callReq))
	if err != nil {
		t.Fatalf("POST /mcp tools/call: %v\n%s", err, output.String())
	}
	callBody, _ := io.ReadAll(callResp.Body)
	callResp.Body.Close()
	if !strings.Contains(string(callBody), "hello from mcp") {
		t.Fatalf("POST /mcp tools/call greet missing expected payload (got: %s)\n%s", callBody, output.String())
	}
}

// Drift gate (a): the published docs must not present the framework as
// unpublished or require a local replace directive. (A docs sibling is
// purging the last occurrences; this asserts the end state.)
//
//gofastr:test-reads /README.md
func TestReadmeDocsNoUnpublishedGuidance(t *testing.T) {
	repoRoot := clitest.RepoRootDir(t)
	forbidden := []string{
		"unpublished",
		"go mod edit -replace github.com/DonaldMurillo/gofastr",
	}
	paths := []string{filepath.Join(repoRoot, "README.md")}
	docsDir := filepath.Join(repoRoot, "framework", "docs", "content")
	err := filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", docsDir, err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(raw))
		for _, needle := range forbidden {
			if strings.Contains(lower, strings.ToLower(needle)) {
				rel, _ := filepath.Rel(repoRoot, path)
				t.Errorf("%s still contains %q — the framework is published; drop local-replace/unpublished guidance", rel, needle)
			}
		}
	}
}
