package appgen

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The executable quickstart: write the tutorial's app spec, run the
// generate pipeline in-process, build the generated app against the
// working tree, boot it, and assert the three claims the tutorial's own
// curl block makes: the entity API answers, the screen renders, and the
// MCP tools are listed.
//
//gofastr:test-reads framework/docs/content/tutorial-app-spec.md
func TestReadmeQuickstartAppSpecRuns(t *testing.T) {
	yamlBlock, _, initBlock := clitest.ReadmeQuickstartBlocks(t)
	repoRoot := clitest.RepoRootDir(t)
	dir := t.TempDir()

	module := clitest.QuickstartModule(t, initBlock)
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module " + module + "\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.yml"), yamlBlock)

	bp, err := appspec.LoadAppSpec(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatalf("README app spec failed to load: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("README app spec failed to generate: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, file.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, full, file.Content)
	}

	appBin := clitest.TestExecutablePath(filepath.Join(dir, "readme-quickstart-app"))
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	buildCmd.Dir = dir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("README quickstart app did not build: %v\n%s", err, output)
	}

	addr := clitest.FreeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(dir, "readme-quickstart.db"),
	)
	// syncBuffer: os/exec copies the child's output from its own goroutines
	// until Wait returns, so reading a plain bytes.Buffer while the app runs
	// races the copier.
	var output clitest.SyncBuffer
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
	// App spec apps mount entity JSON under /api by default (app.api_prefix),
	// leaving bare paths free for HTML screens.
	clitest.WaitForHTTP(t, baseURL+"/api/notes", &output)
	resp, err := http.Get(baseURL + "/api/notes")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/notes = %d, want 200 (public: true)\n%s", resp.StatusCode, output.String())
	}

	// The server-rendered screen from the app spec's `screens:` half.
	screenResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	screenBody, _ := io.ReadAll(screenResp.Body)
	screenResp.Body.Close()
	if screenResp.StatusCode != http.StatusOK || !strings.Contains(string(screenBody), "My Notes") {
		t.Fatalf("GET / = %d — want 200 with the generated screen (tutorial curls `grep \"My Notes\"`)\n%s", screenResp.StatusCode, output.String())
	}

	// The MCP tools the tutorial's third curl lists.
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
