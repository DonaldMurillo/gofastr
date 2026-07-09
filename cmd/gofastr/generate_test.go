package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestRenderGeneratedProjectFromDeclarations(t *testing.T) {
	crud := true
	timestamps := false
	files, err := renderGeneratedProject([]framework.EntityDeclaration{
		{
			Name:       "posts",
			Table:      "posts",
			CRUD:       &crud,
			MCP:        true,
			Timestamps: &timestamps,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true, Max: floatPtrTest(120)},
				{Name: "published", Type: "bool"},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	// register.go (seam) + client/client.go + one file per entity.
	if len(files) != 3 {
		t.Fatalf("files len = %d, want 3 (register.go, client/client.go, posts.go)", len(files))
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[f.name] = f.content
	}

	// register.go is the fixed seam: it declares RegisterAll and the
	// registrar slice, but carries NO entity name — adding an entity never
	// edits it. Owned scaffold carries no "DO NOT EDIT" header (it's yours to
	// edit). See framework/ARCHITECTURE.md.
	register := byName["register.go"]
	for _, want := range []string{
		"func RegisterAll(app *framework.App)",
		"var registrars []registrar",
		"type registrar struct {",
	} {
		if !strings.Contains(register, want) {
			t.Fatalf("register.go missing %q:\n%s", want, register)
		}
	}
	if strings.Contains(register, `app.Entity(`) {
		t.Fatalf("register.go must hold no app.Entity call (it's the seam):\n%s", register)
	}

	// Everything for the "posts" entity — model, columns, repo, events, and
	// its registration — lives in one self-contained posts.go.
	posts := byName["posts.go"]
	for _, want := range []string{
		`app.Entity("posts", framework.EntityConfig{`,
		`Type: schema.String`,
		`CRUD: boolPtr(true)`,
		`MCP: true`,
		`.WithTimestamps(false))`,
		`type Posts struct {`,
		`PostsTitle = framework.NewStringColumn("title")`,
		`PostsPublished = framework.NewBoolColumn("published")`,
		`type PostsRepo struct {`,
		`func NewPostsRepo(app *framework.App) *PostsRepo`,
		`func (r *PostsRepo) Create(ctx context.Context, row *Posts) error`,
		`func (r *PostsRepo) Query() *framework.TypedQuery[Posts]`,
		`WithTx(tx *sql.Tx) *PostsRepo`,
		// self-registration: the file appends itself, so register.go never
		// needs editing when an entity is added.
		`func registerPosts(app *framework.App) {`,
		`func init() {`,
		`registrar{order: 0, fn: registerPosts}`,
	} {
		if !strings.Contains(posts, want) {
			t.Fatalf("posts.go missing %q:\n%s", want, posts)
		}
	}
}

func TestGenerateTypeScriptCommandShowsMigrationError(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "ts")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected generate ts to fail after removal\n%s", output)
	}
	if !strings.Contains(string(output), "TypeScript codegen has been removed") || !strings.Contains(string(output), "codegen.md") {
		t.Fatalf("unexpected generate ts output:\n%s", output)
	}
}

func TestGenerateProjectWithExternalCodegenExtension(t *testing.T) {
	dir := t.TempDir()
	extPath := filepath.Join(dir, "report-extension.sh")
	writeTestFile(t, extPath, `#!/bin/sh
cat >/dev/null
printf '%s' '{"files":[{"path":"report.go","content":"package reports\n"}]}'
`)
	if err := os.Chmod(extPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "reports.codegen.json"), `{"name":"reports"}`)
	writeTestFile(t, filepath.Join(dir, "gofastr.codegen.yml"), `
version: 1
codegen:
  output: generated
  generators:
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
      output: reports
  extensions:
    - name: report-generator
      command: [`+extPath+`]
`)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	generateProject([]string{"--config=gofastr.codegen.yml"})
	data, err := os.ReadFile(filepath.Join("generated", "reports", "report.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package reports\n" {
		t.Fatalf("report.go = %q", data)
	}
}

func TestSafeCleanOutputDirRejectsSymlinkRoot(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "register.go"), []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := safeCleanOutputDir(link)
	if err == nil || !strings.Contains(err.Error(), "refusing to write through symlink") {
		t.Fatalf("safeCleanOutputDir err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "register.go")); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}

func TestBlueprintGenerateRejectsSymlinkOutput(t *testing.T) {
	if os.Getenv("GOFASTR_BLUEPRINT_SYMLINK_HELPER") == "1" {
		generateProject([]string{"--from=gofastr.yml", "--out=out", "--no-clean"})
		return
	}
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outLink := filepath.Join(dir, "out")
	if err := os.Symlink(outside, outLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), `
app:
  name: Demo
entities:
  - name: posts
    fields:
      - name: title
        type: string
`)
	cmd := exec.Command(os.Args[0], "-test.run=TestBlueprintGenerateRejectsSymlinkOutput")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFASTR_BLUEPRINT_SYMLINK_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected symlink output to fail\n%s", output)
	}
	if !strings.Contains(string(output), "refusing to write through symlink") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if entries, err := os.ReadDir(outside); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("blueprint wrote through symlink: %#v", entries)
	}
}

func TestGenerateProjectDryRunJSONValidatesOutputBeforeExtensions(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "extension-ran")
	extPath := filepath.Join(dir, "report-extension.sh")
	writeTestFile(t, extPath, `#!/bin/sh
touch `+marker+`
printf '%s' '{"files":[{"path":"report.go","content":"package reports\n"}]}'
`)
	if err := os.Chmod(extPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "reports.codegen.json"), `{"name":"reports"}`)
	writeTestFile(t, filepath.Join(dir, "gofastr.codegen.yml"), `
version: 1
codegen:
  output: generated
  generators:
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
  extensions:
    - name: report-generator
      command: [`+extPath+`]
`)
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--config="+filepath.Join(dir, "gofastr.codegen.yml"), "--out=..", "--dry-run", "--json")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for unsafe output path\n%s", stdout.String())
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("extension executed before output validation: %v", statErr)
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

func floatPtrTest(v float64) *float64 {
	return &v
}

// copyGoSum copies the repo's go.sum into the temp module so `go test`
// inside the temp dir can satisfy transitive dependencies without needing
// network access.
func copyGoSum(repoRoot, destDir string) error {
	src, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destDir, "go.sum"), src, 0o644)
}

func repoGoVersion(repoRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go directive not found in %s/go.mod", repoRoot)
}
