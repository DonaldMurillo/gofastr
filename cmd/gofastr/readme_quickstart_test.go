package main

// Executable-README gate.
//
// The README's "Declare an app (blueprint)" quickstart is a contract:
// the yaml block must load, validate, generate, build, and serve. These
// tests extract the actual fenced blocks from README.md and execute
// them, so any README edit that breaks the quickstart fails CI loudly.
// The extraction is anchored on purpose — if the section heading, the
// `# gofastr.yml` yaml block, or the documented command sequence moves
// or changes shape, the gate fails and the README must be fixed to stay
// runnable (not the other way around).

import (
	"bytes"
	"context"
	"fmt"
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
)

const readmeBlueprintHeading = "### Declare an app (blueprint)"

// readmeQuickstartHeading anchors the smallest-app Go snippet — the first
// program the README shows. Its section ends at the first ### heading,
// "### Updating GoFastr" (readmeSection splits at the next ## or ###).
const readmeQuickstartHeading = "## Quickstart"

// mcpTrueRe matches the entity-config `MCP:  true,` line without pinning the
// exact gofmt column alignment — `MCP:` followed by ≥1 whitespace then `true`.
// A future struct key that lengthens the column must not break this guard.
var mcpTrueRe = regexp.MustCompile(`MCP:\s+true`)

func repoRootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readmeContent(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootDir(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return string(raw)
}

// readmeSection returns the README text between `heading` and the next
// heading of the same or higher level.
func readmeSection(content, heading string) (string, error) {
	idx := strings.Index(content, heading)
	if idx < 0 {
		return "", fmt.Errorf("README anchor missing: heading %q not found — the executable quickstart gate is pinned to it; if the section was renamed, update readme_quickstart_test.go AND keep the quickstart runnable", heading)
	}
	rest := content[idx+len(heading):]
	end := len(rest)
	for _, next := range []string{"\n## ", "\n### "} {
		if i := strings.Index(rest, next); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end], nil
}

// fencedBlock returns the body of the first ```<lang> fence in section
// whose body contains mustContain.
func fencedBlock(section, lang, mustContain string) (string, error) {
	lines := strings.Split(section, "\n")
	var body []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inFence && trimmed == "```"+lang:
			inFence = true
			body = body[:0]
		case inFence && trimmed == "```":
			inFence = false
			block := strings.Join(body, "\n")
			if strings.Contains(block, mustContain) {
				return block, nil
			}
		case inFence:
			body = append(body, line)
		}
	}
	return "", fmt.Errorf("README anchor missing: no ```%s fence containing %q under %q — the quickstart must keep this block; README edits that drop or rename it break the executable-README gate", lang, mustContain, readmeBlueprintHeading)
}

func readmeQuickstartBlocks(t *testing.T) (yamlBlock, bashBlock string) {
	t.Helper()
	section, err := readmeSection(readmeContent(t), readmeBlueprintHeading)
	if err != nil {
		t.Fatal(err)
	}
	yamlBlock, err = fencedBlock(section, "yaml", "# gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	bashBlock, err = fencedBlock(section, "bash", "gofastr generate --from=gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	return yamlBlock, bashBlock
}

// quickstartModule extracts <module> from the documented
// `go mod init <module>` line.
func quickstartModule(t *testing.T, bashBlock string) string {
	t.Helper()
	for _, line := range strings.Split(bashBlock, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "go" && fields[1] == "mod" && fields[2] == "init" {
			return fields[3]
		}
	}
	t.Fatalf("README quickstart bash block lost its `go mod init <module>` line:\n%s", bashBlock)
	return ""
}

func TestReadmeAnchorMissingFails(t *testing.T) {
	if _, err := readmeSection("# Other\n\nNo quickstart here.\n", readmeBlueprintHeading); err == nil {
		t.Fatal("missing heading did not error")
	}
	if _, err := fencedBlock("### Declare an app (blueprint)\n\n```yaml\nfoo: bar\n```\n", "yaml", "# gofastr.yml"); err == nil {
		t.Fatal("missing anchored fence did not error")
	}
}

func TestReadmeQuickstartShapeIsStable(t *testing.T) {
	yamlBlock, bashBlock := readmeQuickstartBlocks(t)
	for _, want := range []string{"entities:", "type: relation"} {
		if !strings.Contains(yamlBlock, want) {
			t.Fatalf("README blueprint block lost %q — the quickstart yaml must stay a real relation-bearing blueprint:\n%s", want, yamlBlock)
		}
	}
	// `gofastr dev`, not `go run .`: only the dev server hot-reloads, and the
	// quickstart is the development on-ramp. TestReadmeQuickstartBlueprintRuns
	// keeps the sequence executable by building and booting the same app.
	for _, want := range []string{"go mod init", "gofastr generate --from=gofastr.yml", "go mod tidy", "gofastr dev"} {
		if !strings.Contains(bashBlock, want) {
			t.Fatalf("README quickstart command sequence lost %q — the documented path must stay the executable path:\n%s", want, bashBlock)
		}
	}
}

// Drift gate (b): every relation field in the README blueprint must
// target a declared entity. loadBlueprint runs full blueprint
// validation, and the explicit loop keeps the gate meaningful even if
// that validation ever loosens.
func TestReadmeBlueprintRelationsResolve(t *testing.T) {
	yamlBlock, bashBlock := readmeQuickstartBlocks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yamlBlock)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("README blueprint does not validate: %v", err)
	}
	if module := quickstartModule(t, bashBlock); bp.App.Module != module {
		t.Fatalf("README blueprint module %q != quickstart `go mod init %s`", bp.App.Module, module)
	}
	declared := map[string]bool{}
	for _, decl := range bp.Entities {
		declared[decl.Name] = true
	}
	for _, decl := range bp.Entities {
		for _, field := range decl.Fields {
			if !strings.EqualFold(field.Type, "relation") {
				continue
			}
			if !declared[field.To] {
				t.Fatalf("README blueprint: %s.%s relates to undeclared entity %q", decl.Name, field.Name, field.To)
			}
		}
	}
}

// Drift gate (c): the README blueprint must pass the repo's own
// validator end to end — full blueprint validation (loadBlueprint) plus
// the unscoped-PII lint that `gofastr validate` enforces. The README
// can never again ship a quickstart that the validator rejects.
func TestReadmeBlueprintPassesValidator(t *testing.T) {
	yamlBlock, _ := readmeQuickstartBlocks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yamlBlock)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("README.md quickstart blueprint fails validateBlueprint: %v", err)
	}
	for _, f := range lintUnscopedPII(bp) {
		t.Errorf("README.md quickstart blueprint fails `gofastr validate` (unscoped-pii): %s", f.Message())
	}
}

// The executable quickstart: write the README's blueprint, run the
// generate pipeline in-process, build the generated app against the
// working tree, boot it, and hit a CRUD endpoint.
func TestReadmeQuickstartBlueprintRuns(t *testing.T) {
	yamlBlock, bashBlock := readmeQuickstartBlocks(t)
	repoRoot := repoRootDir(t)
	dir := t.TempDir()

	module := quickstartModule(t, bashBlock)
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module " + module + "\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), yamlBlock)

	bp, err := loadBlueprint(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatalf("README blueprint failed to load: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("README blueprint failed to generate: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, full, file.content)
	}

	appBin := testExecutablePath(filepath.Join(dir, "readme-quickstart-app"))
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	buildCmd.Dir = dir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("README quickstart app did not build: %v\n%s", err, output)
	}

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(dir, "readme-quickstart.db"),
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start README quickstart app: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	baseURL := "http://" + addr
	// Blueprint apps mount entity JSON under /api by default (app.api_prefix),
	// leaving bare paths free for HTML screens.
	waitForHTTP(t, baseURL+"/api/posts", &output)
	resp, err := http.Get(baseURL + "/api/posts")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/posts = %d, want 200\n%s", resp.StatusCode, output.String())
	}
}

// readmeGoQuickstart extracts the smallest-app Go program — the first ```go
// fence under "## Quickstart" (the one with a func main).
func readmeGoQuickstart(t *testing.T) string {
	t.Helper()
	section, err := readmeSection(readmeContent(t), readmeQuickstartHeading)
	if err != nil {
		t.Fatal(err)
	}
	block, err := fencedBlock(section, "go", "func main()")
	if err != nil {
		t.Fatalf("README smallest-app Go snippet missing: %v", err)
	}
	return block
}

// TestReadmeGoQuickstartRuns is the executable gate for the smallest-app Go
// snippet. It extracts the exact program, swaps only the hard-coded listen
// address for a free port (the one transform, mirroring the go.mod injection
// the blueprint gate does), compiles it against the working tree, boots it,
// and asserts the runtime claims the README's comments make:
//   - GET /posts == 200 (anonymous read) — Public: true opts out of
//     secure-by-default (crud requireAuthenticated) so the documented
//     "complete server" does not 401.
//   - POST /posts == 201 (anonymous write) — Public: true's comment promises
//     read AND write; this catches a regression where read is open but create
//     silently still requires a session.
//   - POST /mcp initialize returns a JSON-RPC result AND tools/list contains
//     posts_list + posts_create — WithMCP() mounts /mcp AND MCP:true on the
//     entity actually registered its CRUD tools (not just an empty /mcp).
//
// This gate exists because those claims silently drifted from the code
// (issue #65 secure-by-default and the WithMCP requirement) while the snippet
// went untested.
func TestReadmeGoQuickstartRuns(t *testing.T) {
	src := readmeGoQuickstart(t)
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

	repoRoot := repoRootDir(t)
	dir := t.TempDir()
	addr := freeAddr(t)
	src = strings.Replace(src, `":8080"`, `"`+addr+`"`, 1)

	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module readme.example/smallest\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "main.go"), src)

	appBin := testExecutablePath(filepath.Join(dir, "readme-smallest-app"))
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	buildCmd.Dir = dir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("README smallest-app snippet did not build: %v\n%s", err, output)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start README smallest-app: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	baseURL := "http://" + addr
	waitForHTTP(t, baseURL+"/posts", &output)

	// Anonymous READ — the documented "complete server" is reachable, and
	// Public: true opts out of secure-by-default so it does not 401.
	getResp, err := http.Get(baseURL + "/posts")
	if err != nil {
		t.Fatal(err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /posts = %d, want 200 (Public opt-out missing?)\n%s", getResp.StatusCode, output.String())
	}

	// Anonymous WRITE — Public: true's comment promises read AND write, so a
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
	// stateless JSON-RPC over POST — no Mcp-Session-Id threading needed — so
	// initialize then tools/list can be called directly. Asserting the tool
	// names are present (not just that /mcp != 404) catches a regression where
	// WithMCP() still mounts /mcp but MCP:true was dropped from the entity (or
	// tool registration failed past boot), leaving an empty tool set.
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
func TestReadmeDocsNoUnpublishedGuidance(t *testing.T) {
	repoRoot := repoRootDir(t)
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
