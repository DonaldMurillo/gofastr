package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestRenderGeneratedProjectFromDeclarations(t *testing.T) {
	crud := true
	timestamps := false
	files, err := RenderGeneratedProject([]framework.EntityDeclaration{
		{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "posts",
			Table: "posts", Exposure: &framework.ExposureDeclaration{CRUD: &crud, MCP: true}, Timestamps: &timestamps,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true, Max: floatPtrTest(120)},
				{Name: "published", Type: "bool"},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	// register.go (seam) + events.go (event-helper seam) + client/client.go
	// + one file per entity.
	if len(files) != 4 {
		t.Fatalf("files len = %d, want 4 (register.go, events.go, client/client.go, posts.go)", len(files))
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[f.Name] = f.Content
	}

	// register.go is the fixed seam: it declares RegisterAll and the
	// registrar slice, but carries NO entity name. Adding an entity never
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

	// Everything for the "posts" entity lives in one self-contained posts.go:
	// model, columns, repo, events, and its registration.
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
		`Event helpers: OnPostsCreated/OnPostsUpdated/OnPostsDeleted in this package.`,
		`type PostsRepo struct {`,
		`func NewPostsRepo(app *framework.App) *PostsRepo`,
		`func (r *PostsRepo) Create(ctx context.Context, in PostsInput) (*Posts, error)`,
		`func (r *PostsRepo) Update(ctx context.Context, id string, patch PostsPatch) (*Posts, error)`,
		"Published *bool `json:\"published,omitempty\"`",
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

func TestGenerateProjectWithExternalCodegenExtension(t *testing.T) {
	dir := t.TempDir()
	extCommand := clitest.WriteReportExtension(t, dir, "")
	clitest.WriteTestFile(t, filepath.Join(dir, "reports.codegen.json"), `{"name":"reports"}`)
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.settings.yml"), `
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
      command: `+extCommand+`
`)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	GenerateProject([]string{"--config=gofastr.settings.yml"})
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

func TestAppSpecGenerateRejectsSymlinkOutput(t *testing.T) {
	if os.Getenv("GOFASTR_APPSPEC_SYMLINK_HELPER") == "1" {
		RunGenerateApp([]string{"gofastr.yml", "--out=out"})
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
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.yml"), `
app:
  name: Demo
entities:
  - name: posts
    fields:
      - name: title
        type: string
`)
	// os.Executable resolves the test binary absolutely: os.Args[0] can
	// be a bare/relative name, and cmd.Dir points the re-exec at a temp
	// dir, so a relative arg[0] would fail to exec (empty output) under
	// the full package run where a sibling test has moved the cwd.
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	cmd := exec.Command(self, "-test.run=^TestAppSpecGenerateRejectsSymlinkOutput$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFASTR_APPSPEC_SYMLINK_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected symlink output to fail\n%s", output)
	}
	if !strings.Contains(string(output), "refusing to write through symlink") {
		t.Fatalf("unexpected output (err=%v):\n%s", err, output)
	}
	if entries, err := os.ReadDir(outside); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("app spec wrote through symlink: %#v", entries)
	}
}

func floatPtrTest(v float64) *float64 {
	return &v
}
