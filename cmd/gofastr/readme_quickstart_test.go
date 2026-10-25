package main

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// mcpTrueRe matches the entity-config `MCP:  true,` line without pinning the
// exact gofmt column alignment: `MCP:` followed by ≥1 whitespace then `true`.
// A future struct key that lengthens the column must not break this guard.
var mcpTrueRe = regexp.MustCompile(`MCP:\s+true`)

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
	readmeDonaldsWayHeading = "### Donald's Way"
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

// TestReadmeGoQuickstartRuns is the executable gate for the smallest-app Go
// snippet. It extracts the exact program, swaps only the hard-coded listen
// address for a free port (the one transform, mirroring the go.mod injection
// the app spec gate does), compiles it against the working tree, boots it,
// and asserts the runtime claims the README's comments make:
//   - GET /posts == 200 (anonymous read): Public: true opts out of
//     secure-by-default (crud requireAuthenticated) so the documented
//     "complete server" does not 401.
//   - POST /posts == 201 (anonymous write): Public: true's comment promises
//     read AND write; this catches a regression where read is open but create
//     silently still requires a session.
//   - POST /mcp initialize returns a JSON-RPC result AND tools/list contains
//     posts_list + posts_create: WithMCP() mounts /mcp AND MCP:true on the
//     entity actually registered its CRUD tools rather than leaving /mcp empty.
//
// This gate exists because those claims silently drifted from the code
// (issue #65 secure-by-default and the WithMCP requirement) while the snippet
// went untested.
func TestReadmeGoQuickstartRuns(t *testing.T) {
	src := readmeProgram(t, readmeEntityHeading)
	// Guard the three source-level claims so a future edit can't drop the
	// flags and leave the runtime assertions passing for the wrong reason.
	// MCP:true is matched with a regexp (not the exact gofmt-aligned literal)
	// so a future struct-key addition that re-aligns the column does not
	// silently break this guard.
	for _, want := range []string{"framework.WithMCP()", "Public: true"} {
		if !strings.Contains(src, want) {
			t.Fatalf("README smallest-app snippet lost %q — it backs a runtime claim:\n%s", want, src)
		}
	}
	if !mcpTrueRe.MatchString(src) {
		t.Fatalf("README smallest-app snippet lost MCP:true — it backs the /mcp tool-registration claim:\n%s", src)
	}

	baseURL, output := buildAndBootReadmeProgram(t, src, "readme.example/smallest", "readme-smallest-app", "/posts")

	// Anonymous READ: the documented "complete server" is reachable, and
	// Public: true opts out of secure-by-default so it does not 401.
	getResp, err := http.Get(baseURL + "/posts")
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /posts = %d, want 200 (Public opt-out missing?)\n%s", getResp.StatusCode, output.String())
	}

	// Anonymous WRITE: Public: true's comment promises read AND write, so a
	// POST must persist. Catches the regression where read is open but create
	// silently still requires a session (the secure-by-default default).
	postResp, err := http.Post(baseURL+"/posts", "application/json", strings.NewReader(`{"title":"gate"}`))
	if err != nil {
		t.Fatalf("POST /posts: %v\n%s", err, output.String())
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /posts = %d, want 201 (Public: true should grant anonymous write)\n%s", postResp.StatusCode, output.String())
	}

	// MCP is live AND carries the entity CRUD tools the snippet promises
	// (posts_list/get/create/update/delete). The Streamable HTTP transport is
	// stateless JSON-RPC over POST, with no Mcp-Session-Id threading needed, so
	// initialize then tools/list can be called directly. Asserting the tool
	// names are present, rather than only that /mcp != 404, catches a
	// regression where WithMCP() still mounts /mcp but MCP:true was dropped
	// from the entity (or tool registration failed past boot), leaving an
	// empty tool set.
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
	for _, want := range []string{"posts_list", "posts_create"} {
		if !strings.Contains(string(listBody), want) {
			t.Fatalf("POST /mcp tools/list missing %q — MCP:true did not register the entity's CRUD tools (got: %s)\n%s", want, listBody, output.String())
		}
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
