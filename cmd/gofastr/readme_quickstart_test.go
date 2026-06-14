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
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const readmeBlueprintHeading = "### Declare an app (blueprint)"

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
	for _, want := range []string{"go mod init", "gofastr generate --from=gofastr.yml", "go mod tidy", "go run ."} {
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

	appBin := filepath.Join(dir, "readme-quickstart-app")
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
