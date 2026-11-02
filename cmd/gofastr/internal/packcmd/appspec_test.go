package packcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecBlockMultipleActionsAreReachable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
screens:
  - name: home
    route: /
    body:
      - kind: form
        props:
          action: /search
        actions:
          - name: live_search
            event: input
            client_js: "G.updateText('[data-result]', params.value)"
          - name: submit_search
            event: submit
            client_js: "G.updateText('[data-result]', 'submitted')"
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	screens := allScreenContent(mustRenderAppSpecFiles(t, bp))
	clitest.AssertContains(t, screens, `component.On("live_search"`)
	clitest.AssertContains(t, screens, `component.On("submit_search"`)
	clitest.AssertContains(t, screens, `"data-action": "live_search"`)
	clitest.AssertContains(t, screens, `"data-action-type": "input"`)
	clitest.AssertContains(t, screens, `"data-action-submit": "submit_search"`)
}

func TestRenderAppSpecFilesContentCoversAllSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.TestAppSpecYAML())
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	byName := filesByName(files)
	postsFile := filepath.Join("entities", "posts.go")
	clitest.AssertContains(t, byName[postsFile], `app.Entity("posts", framework.EntityConfig{`)
	clitest.AssertContains(t, byName[postsFile], `CursorField: "id"`)
	clitest.AssertContains(t, byName[postsFile], `CursorFields: []string{"created_at", "id"}`)
	clitest.AssertContains(t, byName[postsFile], `Indices: []framework.Index{`)
	clitest.AssertContains(t, byName[postsFile], `Properties: map[string]any{"icon": "newspaper", "label": "Posts"}`)
	clitest.AssertContains(t, byName[postsFile], `type Posts struct`)
	// A top-level entity_list makes the screen a server-rendered ContextOnly
	// screen that renders the list via the resource engine (not a client island).
	screenContent := allScreenContent(files)
	clitest.AssertContains(t, screenContent, `type HomeScreen struct{ component.ContextOnly }`)
	clitest.AssertContains(t, screenContent, `func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {`)
	clitest.AssertContains(t, screenContent, `html.Heading(html.HeadingConfig{Level: 1`)
	clitest.AssertContains(t, screenContent, `ui.Link(ui.LinkConfig{Href: "/docs/", Text: "Docs", Class: "docs-link"})`)
	clitest.AssertContains(t, screenContent, `ui.Section(ui.SectionConfig{`)
	clitest.AssertContains(t, screenContent, `island.NewIsland("live_status"`)
	clitest.AssertContains(t, screenContent, `component.NewWidget("save_button"`)
	clitest.AssertContains(t, screenContent, `func (s *HomeScreen) ComponentID() string { return "screen-home" }`)
	clitest.AssertContains(t, screenContent, `component.On("save_click"`)
	clitest.AssertContains(t, screenContent, `"data-action": "save_click"`)
	// The list renders through the appUI builder; the list follows the
	// screen's heading block, so its title drops to a section (level 2).
	clitest.AssertContains(t, screenContent, `appUI.List("posts").Columns("title", "status").PageSize(5).NoCreate().NoLinks().Heading("Latest posts", 2).Empty("No posts yet.")`)
	clitest.AssertContains(t, byName[filepath.Join("entities", "posts_endpoints.go")], `func PublishPost(ctx context.Context, in PublishPostInput) (*Posts, error)`)
	clitest.AssertContains(t, byName["stubs.go"], `func RequestLoggerMiddleware(next http.Handler) http.Handler`)
	clitest.AssertContains(t, byName["stubs.go"], `type AnalyticsPlugin struct{}`)
	clitest.AssertContains(t, byName["stubs.go"], `func NormalizeSlug()`)
	clitest.AssertContains(t, screenContent, `site.Register("/", &HomeScreen{}, nil)`)
	clitest.AssertContains(t, byName["app.go"], `appName = "Demo"`)
	clitest.AssertContains(t, byName["app.go"], `appModule = "example.com/demo"`)
	clitest.AssertContains(t, byName["app.go"], `dbDriver = "sqlite"`)
	clitest.AssertContains(t, byName["app.go"], `staticDir = "public"`)
	clitest.AssertContains(t, byName["app.go"], `fwApp.Use(RequestLoggerMiddleware)`)
	clitest.AssertContains(t, byName["app.go"], `fwApp.RegisterPlugin(AnalyticsPlugin{})`)
	clitest.AssertContains(t, byName["main.go"], `entities.RegisterAll(fwApp)`)
	clitest.AssertContains(t, byName["main.go"], `RegisterGenerated(fwApp, site, db)`)
	clitest.AssertContains(t, byName["main.go"], `framework.WithMCP(),`)
	clitest.AssertContains(t, byName["main.go"], `framework.WithMCPIntrospection(),`)
	clitest.AssertContains(t, byName["main.go"], `framework.WithMCPTools(mcptools.Register),`)
	clitest.AssertContains(t, byName["main.go"], `"github.com/DonaldMurillo/gofastr/framework/docs/mcptools"`)
	clitest.AssertContains(t, byName["main.go"], `fwApp.RegisterPlugin(gflog.New(gflog.Config{}))`)
	clitest.AssertContains(t, byName["main.go"], `uihost.WithStaticDir("public")`)
	clitest.AssertContains(t, byName["main.go"], `"github.com/DonaldMurillo/gofastr/framework/isolation"`)
	clitest.AssertContains(t, byName["main.go"], `runtimeIsolation, err := isolation.Resolve(".")`)
	clitest.AssertContains(t, byName["main.go"], `runtimeIsolation.Database(driver, dsn)`)
	clitest.AssertContains(t, byName["main.go"], `runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))`)
}

func filesByName(files []appgen.GeneratedFile) map[string]string {
	out := map[string]string{}
	for _, file := range files {
		out[file.Name] = file.Content
	}
	return out
}

// allScreenContent concatenates every screen-bearing generated file. Screen
// structs, mounts, and resource wiring now live in per-screen files
// (screen_*.go) instead of one screens.go, so layout-agnostic content
// assertions read across all of them. Includes legacy screens.go if present.
func allScreenContent(files []appgen.GeneratedFile) string {
	var b strings.Builder
	for _, f := range files {
		if strings.HasPrefix(f.Name, "screen_") || f.Name == "screens.go" {
			// The words a screen says are phrases; read them as English.
			b.WriteString(inlinePhrases(f.Content))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func mustRenderAppSpecFiles(t *testing.T, bp appspec.AppSpec) []appgen.GeneratedFile {
	t.Helper()
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	return files
}
