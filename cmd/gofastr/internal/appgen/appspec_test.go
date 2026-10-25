package appgen

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecGroupedEntityConfigsDecodeAndGenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Grouped
entities:
  - name: notes
    scope:
      owner_field: user_id
      soft_delete: true
    pagination:
      cursor_fields: [created_at, id]
      max_list_limit: 50
    exposure:
      crud: false
      mcp: true
      access:
        read: notes:read
    fields:
      - name: title
        type: string
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	decl := bp.Entities[0]
	if decl.Scope == nil || decl.Pagination == nil || decl.Exposure == nil {
		t.Fatalf("grouped declarations not decoded: %#v", decl)
	}
	generated, err := RenderEntityRegistration(decl)
	if err != nil {
		t.Fatalf("renderEntityRegistration: %v", err)
	}
	for _, want := range []string{
		"Scope: &framework.ScopeConfig{", `OwnerField: "user_id"`,
		"Pagination: &framework.PaginationConfig{", "MaxListLimit: 50",
		"Exposure: &framework.ExposureConfig{", "CRUD: boolPtr(false)",
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated entity missing %q:\n%s", want, generated)
		}
	}
}

// An app spec endpoint becomes the settings entry, a typed handler stub
// that answers 501, the registration and a typed client call: the app
// generate app writes is the one `gofastr generate` keeps current.
func TestAppSpecEndpointWritesSettingsAndStub(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish_post
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#PublishPost
    description: Publish a draft.
    mcp: true
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	byName := filesByName(mustRenderAppSpecFiles(t, bp))
	clitest.AssertContains(t, byName["gofastr.settings.yml"], "  - name: \"publish_post\"\n    entity: \"posts\"\n    method: POST\n    path: \"{id}/publish\"\n")
	stub := byName[filepath.Join("entities", "posts_endpoints.go")]
	clitest.AssertContains(t, stub, "type PublishPostInput struct {\n\tID string `path:\"id\" json:\"-\"`\n}")
	clitest.AssertContains(t, stub, "func PublishPost(ctx context.Context, in PublishPostInput) (*Posts, error) {")
	clitest.AssertContains(t, stub, "handler.Errorf(http.StatusNotImplemented")
	reg := byName[filepath.Join("entities", "endpoints.gen.go")]
	clitest.AssertContains(t, reg, `app.EntityEndpoints("posts", framework.Endpoint{`)
	clitest.AssertContains(t, reg, "MCPHandler: handler.ToolAdapter[PublishPostInput, *Posts](PublishPost),")
	clitest.AssertContains(t, byName[filepath.Join("entities", "client", "client.go")], "PublishPost(")
	if strings.Contains(byName["app.go"], "Router().Handle") {
		t.Fatalf("app.go still hand-registers a route:\n%s", byName["app.go"])
	}
}

func TestAppSpecPublicOpenAPIEmitsExplicitFrameworkOption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, "app:\n  name: Docs\n  module: example.com/docs\n  public_openapi: true\n")
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	got := RenderAppSpecMain(bp)
	clitest.AssertContains(t, got, `framework.WithPublicOpenAPI(),`)
}

func TestAppSpecAuthMountsSessionMW(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo", Auth: appspec.AppSpecAuth{Enabled: true}},
	}
	got := RenderAppSpecApp(bp)
	// Without the session middleware, cookie-authenticated requests never
	// reach owner/access-scoped CRUD: anonymous AND authorized are both 401.
	clitest.AssertContains(t, got, `fwApp.Use(auth.SessionMiddleware(authMgr))`)
	// The middleware must be mounted after the manager is initialized.
	if mw, init := strings.Index(got, "auth.SessionMiddleware(authMgr)"), strings.Index(got, "authMgr.Init(fwApp)"); mw < init {
		t.Errorf("SessionMiddleware mounted before authMgr.Init:\n%s", got)
	}
}

func TestAuthDevModeFalseHonored(t *testing.T) {
	bp, err := covT_decode(t, "app:\n  name: D\n  auth:\n    enabled: true\n    dev_mode: false\n")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bp.App.Auth.DevMode {
		t.Fatal("explicit dev_mode: false was not honored")
	}
	got := RenderAppSpecApp(bp)
	if strings.Contains(got, "DevMode: true") {
		t.Errorf("dev_mode: false still emitted DevMode: true:\n%s", got)
	}
	clitest.AssertContains(t, got, "DevMode: false")
	// The prod-mode branch is not covered by the flagship build (which
	// uses the dev default). Prove it still renders valid Go.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "app.go", got, parser.AllErrors); err != nil {
		t.Fatalf("app.go is not valid Go: %v\n%s", err, got)
	}
}

func TestAuthDevModeWarnsInGenCode(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo", Auth: appspec.AppSpecAuth{Enabled: true, DevMode: true}},
	}
	got := RenderAppSpecApp(bp)
	// The dev-mode wiring must announce itself and say how to turn it off.
	clitest.AssertContains(t, got, "DEV MODE")
	clitest.AssertContains(t, got, "dev_mode: false")
}

func TestAuthCSRFGapCommentEmitted(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo", Auth: appspec.AppSpecAuth{Enabled: true, DevMode: true}},
	}
	got := RenderAppSpecApp(bp)
	// auth.CSRF is deliberately not mounted (it would 403 the JSON
	// REST/MCP surface); the generated code must say so and point at
	// the docs for apps that add browser forms.
	clitest.AssertContains(t, got, "auth.CSRF")
}

func TestGenerateWarnsAuthDevMode(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, "app:\n  name: Demo\n  module: example.com/demo\n  auth:\n    enabled: true\n")
	output := clitest.CaptureStdout(t, func() {
		RunGenerateApp([]string{path, "--dry-run"})
	})
	if !strings.Contains(output, "dev mode") {
		t.Errorf("generate did not warn about auth dev mode; output:\n%s", output)
	}
}

// TestGenerateWarnsPublicEntities pins issue #65's item 2: `gofastr
// generate` loudly lists every entity left publicly readable/writable
// (public: true) so the open surface of a generated app is never silent.
func TestGenerateWarnsPublicEntities(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: announcements
    public: true
    fields:
      - name: title
        type: string
        required: true
`)
	output := clitest.CaptureStdout(t, func() {
		RunGenerateApp([]string{path, "--dry-run"})
	})
	if !strings.Contains(output, "announcements") || !strings.Contains(output, "public: true") {
		t.Errorf("generate did not warn about the public entity; output:\n%s", output)
	}
}

func TestGenerateFromAppSpecDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	// Run from the app spec's own directory; generating from the repo cwd
	// would (correctly) trip the app.module vs enclosing-go.mod check.
	clitest.CovT_chdir(t, dir)
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.TestAppSpecYAML())
	output := clitest.CaptureStdout(t, func() {
		RunGenerateApp([]string{path, "--dry-run", "--json"})
	})
	var got struct {
		Files []struct {
			Path string `json:"path"`
			Size int    `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("dry-run JSON did not parse: %v\n%s", err, output)
	}
	paths := map[string]bool{}
	for _, file := range got.Files {
		paths[file.Path] = true
		if file.Size == 0 {
			t.Fatalf("file %s has zero size", file.Path)
		}
	}
	for _, want := range []string{
		"main.go",
		filepath.Join("entities", "register.go"),
		filepath.Join("entities", "posts.go"),
		"app.go",
		"screens_register.go",
		"stubs.go",
	} {
		if !paths[want] {
			t.Fatalf("dry-run paths missing %s: %#v", want, paths)
		}
	}
}

func TestRenderAppSpecFilesGeneratedPackagesBuild(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	// testAppSpecYAML declares module example.com/demo; scaffold the flat
	// package main into a gen/ subpackage of that module so main.go's entities
	// import (example.com/demo/gen/entities) resolves.
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	bp.App.OutputDir = "gen"
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, "gen", file.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// -short skips the emitted e2e_test.go (which builds + boots the binary);
	// this test only proves the generated packages compile + their unit tests pass.
	cmd := exec.Command("go", "test", "-short", "-mod=mod", "./gen/entities", "./gen")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated packages did not build: %v\n%s", err, output)
	}
}

func filesByName(files []GeneratedFile) map[string]string {
	out := map[string]string{}
	for _, file := range files {
		out[file.Name] = file.Content
	}
	return out
}

func mustRenderAppSpecFiles(t *testing.T, bp appspec.AppSpec) []GeneratedFile {
	t.Helper()
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	return files
}
