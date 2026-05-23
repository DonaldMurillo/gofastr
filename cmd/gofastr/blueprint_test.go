package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestLoadBlueprintDecodesCodegenSurface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if bp.App.Name != "Demo" {
		t.Fatalf("App.Name = %q", bp.App.Name)
	}
	if bp.App.Theme["background"] != "#101820" || bp.App.Theme["primary"] != "#F2AA4C" {
		t.Fatalf("App.Theme = %#v", bp.App.Theme)
	}
	if len(bp.Entities) != 2 {
		t.Fatalf("entities len = %d, want 2", len(bp.Entities))
	}
	if len(bp.Screens) != 1 || bp.Screens[0].Route != "/" {
		t.Fatalf("screens = %#v", bp.Screens)
	}
	if len(bp.Endpoints) != 1 || bp.Endpoints[0].Handler != "publishPost" {
		t.Fatalf("endpoints = %#v", bp.Endpoints)
	}
	if len(bp.Middleware) != 1 || len(bp.Plugins) != 1 || len(bp.Helpers) != 1 {
		t.Fatalf("stubs not decoded: middleware=%d plugins=%d helpers=%d", len(bp.Middleware), len(bp.Plugins), len(bp.Helpers))
	}
}

func TestBlueprintRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte("wat: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "wat"`) {
		t.Fatalf("loadBlueprint err = %v", err)
	}
}

func TestLoadBlueprintSupportsJSONInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.json")
	if err := os.WriteFile(path, []byte(`{"app":{"name":"Demo"},"entities":[{"name":"posts","fields":[{"name":"title","type":"string"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if bp.App.Name != "Demo" || len(bp.Entities) != 1 || bp.Entities[0].Name != "posts" {
		t.Fatalf("decoded blueprint = %#v", bp)
	}
}

func TestLoadBlueprintMergesDirectoryInStableOrder(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "01-app.yml"), `
app:
  name: DirectoryDemo
`)
	writeTestFile(t, filepath.Join(dir, "02-entities.yml"), `
entities:
  - name: users
    fields:
      - name: email
        type: string
`)
	writeTestFile(t, filepath.Join(dir, "03-screens.yml"), `
screens:
  - name: home
    route: /
    title: Home
`)
	bp, err := loadBlueprint(dir)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if bp.App.Name != "DirectoryDemo" || len(bp.Entities) != 1 || len(bp.Screens) != 1 {
		t.Fatalf("merged blueprint = %#v", bp)
	}
}

func TestLoadBlueprintDirectoryValidatesAfterMerge(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "01-users.yml"), `
entities:
  - name: users
    fields:
      - name: email
        type: string
`)
	writeTestFile(t, filepath.Join(dir, "02-posts.yml"), `
entities:
  - name: posts
    fields:
      - name: title
        type: string
      - name: author_id
        type: relation
        to: users
    relations:
      - name: author
        entity: users
        foreign_key: author_id
`)
	bp, err := loadBlueprint(dir)
	if err != nil {
		t.Fatalf("loadBlueprint split directory: %v", err)
	}
	if len(bp.Entities) != 2 {
		t.Fatalf("entities = %#v", bp.Entities)
	}
}

func TestLoadBlueprintRejectsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "README.md"), "no blueprints here\n")
	_, err := loadBlueprint(dir)
	if err == nil || !strings.Contains(err.Error(), "does not contain any blueprint files") {
		t.Fatalf("loadBlueprint err = %v", err)
	}
}

func TestBlueprintValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		yml  string
		want string
	}{
		{
			name: "bad app theme token",
			yml: `
app:
  name: Demo
  theme:
    not-a-token: "#fff"
`,
			want: `unsupported color token "not-a-token"`,
		},
		{
			name: "duplicate entities",
			yml: `
entities:
  - name: posts
    fields: []
  - name: posts
    fields: []
`,
			want: `duplicate entity "posts"`,
		},
		{
			name: "bad relation target",
			yml: `
entities:
  - name: posts
    fields: []
    relations:
      - name: author
        entity: users
`,
			want: `targets unknown entity "users"`,
		},
		{
			name: "duplicate routes",
			yml: `
screens:
  - name: home
    route: /
  - name: dashboard
    route: /
`,
			want: `duplicate screen route "/"`,
		},
		{
			name: "bad screen type",
			yml: `
screens:
  - name: home
    route: /
    type: toast
`,
			want: `unknown screen type "toast"`,
		},
		{
			name: "bad block type",
			yml: `
screens:
  - name: home
    route: /
    body:
      - type: chart
        text: nope
`,
			want: `unsupported block type "chart"`,
		},
		{
			name: "entity list unknown entity",
			yml: `
entities:
  - name: posts
    crud: false
    fields:
      - name: title
        type: string
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: comments
        fields: [body]
`,
			want: `entity_list targets unknown entity "comments"`,
		},
		{
			name: "entity list target must have crud",
			yml: `
entities:
  - name: posts
    crud: false
    fields:
      - name: title
        type: string
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`,
			want: `entity_list target "posts" must enable crud`,
		},
		{
			name: "entity list field must exist",
			yml: `
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
        fields: [missing]
`,
			want: `entity_list field "missing" is not defined on entity "posts"`,
		},
		{
			name: "bad ui action event",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: button
        props:
          text: Save
        actions:
          - name: save
            event: mouseover
            client_js: "G.toast('saved')"
`,
			want: `event "mouseover" is not supported`,
		},
		{
			name: "missing ui action client js",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: button
        props:
          text: Save
        actions:
          - name: save
            event: click
`,
			want: `client_js is required`,
		},
		{
			name: "duplicate ui actions",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: div
        children:
          - kind: button
            props:
              text: One
            actions:
              - name: save
                client_js: "G.toast('one')"
          - kind: button
            props:
              text: Two
            actions:
              - name: save
                client_js: "G.toast('two')"
`,
			want: `duplicate action "save"`,
		},
		{
			name: "duplicate ui action event on same block",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: button
        props:
          text: Save
        actions:
          - name: save_one
            event: click
            client_js: "G.toast('one')"
          - name: save_two
            event: click
            client_js: "G.toast('two')"
`,
			want: `duplicate event "click" on one block`,
		},
		{
			name: "click action must be first when combined",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: input
        props:
          name: q
        actions:
          - name: live_search
            event: input
            client_js: "G.updateText('[data-result]', params.value)"
          - name: focus_click
            event: click
            client_js: "G.toast('clicked')"
`,
			want: `click action must be first`,
		},
		{
			name: "link missing href",
			yml: `
screens:
  - name: home
    route: /
    body:
      - type: link
        text: Docs
`,
			want: `link block href is required`,
		},
		{
			name: "bad method",
			yml: `
endpoints:
  - name: publish
    method: WAT
    path: /publish
    handler: publish
`,
			want: `method "WAT" is not supported`,
		},
		{
			name: "missing handler",
			yml: `
endpoints:
  - name: publish
    method: POST
    path: /publish
`,
			want: `handler is required`,
		},
		{
			name: "bad endpoint entity",
			yml: `
endpoints:
  - name: publish
    method: POST
    path: /publish
    entity: posts
    handler: publish
`,
			want: `targets unknown entity "posts"`,
		},
		{
			name: "duplicate endpoint route",
			yml: `
endpoints:
  - name: publish
    method: POST
    path: /posts/{id}/publish
    handler: publishPost
  - name: publish_again
    method: post
    path: /posts/{id}/publish
    handler: publishAgain
`,
			want: `duplicate endpoint route "POST /posts/{id}/publish"`,
		},
		{
			name: "endpoint collides with crud route",
			yml: `
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
endpoints:
  - name: custom_create
    method: POST
    path: /posts
    handler: customCreate
`,
			want: `collides with generated CRUD route "POST /posts"`,
		},
		{
			name: "endpoint collides with kebab entity crud route",
			yml: `
entities:
  - name: blog-post
    crud: true
    fields:
      - name: title
        type: string
endpoints:
  - name: custom_create
    method: POST
    path: /blog_post
    handler: customCreate
`,
			want: `collides with generated CRUD route "POST /blog_post"`,
		},
		{
			name: "duplicate endpoint handlers",
			yml: `
endpoints:
  - name: publish
    method: POST
    path: /publish
    handler: shared
  - name: archive
    method: POST
    path: /archive
    handler: shared
`,
			want: `duplicate endpoint handler "shared"`,
		},
		{
			name: "endpoint mcp unsupported",
			yml: `
endpoints:
  - name: publish
    method: POST
    path: /publish
    handler: publish
    mcp: true
`,
			want: `cannot set mcp=true`,
		},
		{
			name: "duplicate middleware",
			yml: `
middleware:
  - auth
  - auth
`,
			want: `duplicate middleware "auth"`,
		},
		{
			name: "bad helper identifier",
			yml: `
helpers:
  - "!!!"
`,
			want: `does not produce a valid Go identifier`,
		},
		{
			name: "unknown nested key",
			yml: `
app:
  name: Demo
  surprise: nope
`,
			want: `unknown key "surprise" in app`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.yml")
			writeTestFile(t, path, tc.yml)
			_, err := loadBlueprint(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadBlueprint err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestEntityOwnedEndpointGeneratesStubAndRegistration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
entities:
  - name: posts
    fields:
      - name: title
        type: string
    endpoints:
      - name: publish_post
        method: POST
        path: "{id}/publish"
        handler: publishPost
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if len(bp.Endpoints) != 1 || bp.Endpoints[0].Entity != "posts" {
		t.Fatalf("endpoints = %#v", bp.Endpoints)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	byName := filesByName(files)
	for _, want := range []string{
		"func PublishPost(w http.ResponseWriter, r *http.Request)",
		`fwApp.Router().Handle("POST", "/posts/{id}/publish", http.HandlerFunc(PublishPost))`,
	} {
		if !strings.Contains(byName[filepath.Join("blueprint", "stubs.go")]+byName[filepath.Join("blueprint", "app.go")], want) {
			t.Fatalf("generated files missing %q\napp:\n%s\nstubs:\n%s", want, byName[filepath.Join("blueprint", "app.go")], byName[filepath.Join("blueprint", "stubs.go")])
		}
	}
}

func TestBlueprintKeepsEntityOwnedAndTopLevelEndpoints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
entities:
  - name: posts
    fields:
      - name: title
        type: string
    endpoints:
      - name: publish_post
        method: POST
        path: "{id}/publish"
        handler: publishPost
endpoints:
  - name: health_check
    method: GET
    path: /health
    handler: healthCheck
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if len(bp.Endpoints) != 2 {
		t.Fatalf("endpoints = %#v", bp.Endpoints)
	}
	byName := filesByName(mustRenderBlueprintFiles(t, bp))
	generated := byName[filepath.Join("blueprint", "app.go")] + byName[filepath.Join("blueprint", "stubs.go")]
	assertContains(t, generated, `fwApp.Router().Handle("POST", "/posts/{id}/publish", http.HandlerFunc(PublishPost))`)
	assertContains(t, generated, `fwApp.Router().Handle("GET", "/health", http.HandlerFunc(HealthCheck))`)
}

func TestBlueprintBlockMultipleActionsAreReachable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	screens := filesByName(mustRenderBlueprintFiles(t, bp))[filepath.Join("blueprint", "screens.go")]
	assertContains(t, screens, `component.On("live_search"`)
	assertContains(t, screens, `component.On("submit_search"`)
	assertContains(t, screens, `"data-action": "live_search"`)
	assertContains(t, screens, `"data-action-type": "input"`)
	assertContains(t, screens, `"data-action-submit": "submit_search"`)
}

func TestRenderBlueprintFilesContentCoversAllSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, testBlueprintYAML())
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	byName := filesByName(files)
	assertContains(t, byName[filepath.Join("entities", "register.go")], `app.Entity("posts", framework.EntityConfig{`)
	assertContains(t, byName[filepath.Join("entities", "register.go")], `CursorField: "id"`)
	assertContains(t, byName[filepath.Join("entities", "register.go")], `CursorFields: []string{"created_at", "id"}`)
	assertContains(t, byName[filepath.Join("entities", "register.go")], `Indices: []framework.Index{`)
	assertContains(t, byName[filepath.Join("entities", "register.go")], `Properties: map[string]any{"icon": "newspaper", "label": "Posts"}`)
	assertContains(t, byName[filepath.Join("entities", "models.go")], `type Posts struct`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `type HomeScreen struct{}`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `html.Heading(html.HeadingConfig{Level: 1`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `html.Link(html.LinkConfig{Href: "/docs/", Text: "Docs", Class: "docs-link"})`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `kilnrender.RenderNode(world.Node{Kind: "section"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `island.NewIsland("live_status"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `component.NewWidget("save_button"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `func (s *HomeScreen) ComponentID() string { return "screen-home" }`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `component.On("save_click"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `component.On("entity_list_home_posts_7"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `"data-action": "save_click"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `"data-entity-list": "posts"`)
	assertContains(t, byName[filepath.Join("blueprint", "screens.go")], `"data-entity-list-refresh": "posts"`)
	assertContains(t, byName[filepath.Join("blueprint", "stubs.go")], `func PublishPost(w http.ResponseWriter, r *http.Request)`)
	assertContains(t, byName[filepath.Join("blueprint", "stubs.go")], `func RequestLoggerMiddleware(next http.Handler) http.Handler`)
	assertContains(t, byName[filepath.Join("blueprint", "stubs.go")], `type AnalyticsPlugin struct{}`)
	assertContains(t, byName[filepath.Join("blueprint", "stubs.go")], `func NormalizeSlug()`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `site.Register("/", &HomeScreen{}, nil)`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `BlueprintAppName = "Demo"`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `BlueprintModule = "example.com/demo"`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `BlueprintDBDriver = "sqlite"`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `BlueprintStaticDir = "public"`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `fwApp.Router().Handle("POST", "/posts/{id}/publish", http.HandlerFunc(PublishPost))`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `fwApp.Use(RequestLoggerMiddleware)`)
	assertContains(t, byName[filepath.Join("blueprint", "app.go")], `fwApp.RegisterPlugin(AnalyticsPlugin{})`)
	assertContains(t, byName["main.go"], `entities.RegisterAll(fwApp)`)
	assertContains(t, byName["main.go"], `blueprint.RegisterGenerated(fwApp, site)`)
	assertContains(t, byName["main.go"], `fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)`)
	assertContains(t, byName["main.go"], `uihost.WithStaticDir("public")`)
	assertContains(t, byName["main.go"], `"github.com/DonaldMurillo/gofastr/framework/isolation"`)
	assertContains(t, byName["main.go"], `runtimeIsolation, err := isolation.Resolve(".")`)
	assertContains(t, byName["main.go"], `runtimeIsolation.Database(driver, dsn)`)
	assertContains(t, byName["main.go"], `runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))`)
}

func TestGenerateFromBlueprintDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, testBlueprintYAML())
	output := captureStdout(t, func() {
		generateProject([]string{"--from=" + path, "--dry-run", "--json"})
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
		filepath.Join("entities", "models.go"),
		filepath.Join("blueprint", "app.go"),
		filepath.Join("blueprint", "screens.go"),
		filepath.Join("blueprint", "stubs.go"),
	} {
		if !paths[want] {
			t.Fatalf("dry-run paths missing %s: %#v", want, paths)
		}
	}
}

func TestGenerateFromBlueprintDryRunJSONReportsValidationErrors(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "bad.yml"), `
endpoints:
  - name: bad
    method: WAT
    path: /bad
    handler: bad
`)
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from="+filepath.Join(dir, "bad.yml"), "--dry-run", "--json")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for invalid blueprint\n%s", stdout.String())
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
	if len(got.Files) != 0 || len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, `method "WAT" is not supported`) {
		t.Fatalf("unexpected dry-run JSON error payload: %#v", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".gofastr")); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run with validation errors wrote output dir: %v", statErr)
	}
}

func TestGenerateFromBlueprintDryRunJSONReportsUnsafeOutputPath(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), `
app:
  name: Demo
`)
	cmd := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from="+filepath.Join(dir, "gofastr.yml"), "--out=..", "--dry-run", "--json")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
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

func TestRenderBlueprintFilesGeneratedPackagesBuild(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/blueprint\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, ".gofastr", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-mod=mod", "./.gofastr/entities", "./.gofastr/blueprint")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated blueprint packages did not build: %v\n%s", err, output)
	}
}

func TestBlueprintCLIGeneratesEntireWorkingAppE2E(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), testBlueprintYAML())
	if err := os.Mkdir(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "public", "hello.txt"), "static from generated app")

	runGoFastr := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from=gofastr.yml")
	runGoFastr.Dir = dir
	if output, err := runGoFastr.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(dir, ".gofastr", "main.go")); err != nil {
		t.Fatalf("generated app entrypoint missing: %v", err)
	}

	appBin := filepath.Join(dir, "generated-blueprint-app")
	buildCmd := exec.Command("go", "build", "-mod=mod", "-o", appBin, "./.gofastr")
	buildCmd.Dir = dir
	buildCmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build generated app failed: %v\n%s", err, output)
	}

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, appBin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(dir, "blueprint-e2e.db"),
	)
	var output bytes.Buffer
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
	waitForHTTP(t, baseURL+"/", &output)
	checkBodyContains(t, baseURL+"/", http.StatusOK, "Generated from YAML.")
	checkBodyContains(t, baseURL+"/", http.StatusOK, `data-island="live_status"`)
	checkBodyContains(t, baseURL+"/", http.StatusOK, `data-widget="save_button"`)
	checkBodyContains(t, baseURL+"/", http.StatusOK, "details-section")
	checkBodyContains(t, baseURL+"/hello.txt", http.StatusOK, "static from generated app")

	created := requestJSON(t, http.MethodPost, baseURL+"/posts", map[string]any{"title": "HTTP Post", "status": "draft"}, http.StatusCreated)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("created id = %#v", created["id"])
	}
	got := requestJSON(t, http.MethodGet, baseURL+"/posts/"+id, nil, http.StatusOK)
	if got["title"] != "HTTP Post" {
		t.Fatalf("get title = %#v", got["title"])
	}
	updated := requestJSON(t, http.MethodPut, baseURL+"/posts/"+id, map[string]any{"title": "HTTP Post Updated", "status": "published"}, http.StatusOK)
	if updated["status"] != "published" {
		t.Fatalf("updated status = %#v", updated["status"])
	}
	list := requestJSON(t, http.MethodGet, baseURL+"/posts?limit=10", nil, http.StatusOK)
	data, ok := list["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("list data = %#v", list["data"])
	}
	runBrowserUIE2E(t, baseURL, "HTTP Post Updated")
	_ = requestJSON(t, http.MethodDelete, baseURL+"/posts/"+id, nil, http.StatusNoContent)
	resp404, err := http.Get(baseURL + "/posts/" + id)
	if err != nil {
		t.Fatal(err)
	}
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted get status = %d", resp404.StatusCode)
	}

	openapi := requestJSON(t, http.MethodGet, baseURL+"/openapi.json", nil, http.StatusOK)
	paths := openapi["paths"].(map[string]any)
	if paths["/posts"] == nil || paths["/posts/{id}"] == nil {
		t.Fatalf("openapi paths missing posts routes: %#v", paths)
	}

	resp, err := http.Post(baseURL+"/posts/123/publish", "text/plain", nil)
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

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitForHTTP(t *testing.T, url string, output *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s\n%s", url, output.String())
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
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	ctx, cancel := context.WithTimeout(browserCtx, 20*time.Second)
	defer cancel()

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
		chromedp.WaitVisible(`[data-entity-list="posts"]`, chromedp.ByQuery),
		chromedp.Text(`[data-action-result]`, &before, chromedp.ByQuery),
		chromedp.Click(`#save-action`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Text(`[data-action-result]`, &after, chromedp.ByQuery),
		chromedp.Evaluate(`document.body.getAttribute('data-blueprint-clicked') || ''`, &clicked),
		chromedp.Click(`[data-entity-list-refresh="posts"]`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Text(`[data-entity-list-body]`, &entityListBody, chromedp.ByQuery),
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
	if !strings.Contains(entityListBody, wantEntityTitle) || !strings.Contains(entityListBody, "published") {
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func filesByName(files []generatedFile) map[string]string {
	out := map[string]string{}
	for _, file := range files {
		out[file.name] = file.content
	}
	return out
}

func mustRenderBlueprintFiles(t *testing.T, bp Blueprint) []generatedFile {
	t.Helper()
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	return files
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func testBlueprintYAML() string {
	return `
app:
  name: Demo
  module: example.com/demo
  static_dir: public
  theme:
    background: "#101820"
    primary: "#F2AA4C"
    text: "#F7F4EA"
  db:
    driver: sqlite
    url: file:demo.db
entities:
  - name: users
    fields:
      - name: email
        type: string
        required: true
        unique: true
  - name: posts
    crud: true
    mcp: true
    cursor_field: id
    cursor_fields: [created_at, id]
    indices:
      - name: idx_posts_status
        columns: [status]
        unique: false
    properties:
      label: Posts
      icon: newspaper
    fields:
      - name: title
        type: string
        required: true
        max: 120
      - name: status
        type: enum
        values: [draft, published]
      - name: author_id
        type: relation
        to: users
    relations:
      - type: belongs_to
        name: author
        entity: users
        foreign_key: author_id
screens:
  - name: home
    route: /
    title: Home
    description: Demo homepage
    body:
      - type: heading
        level: 1
        text: Demo
      - type: paragraph
        text: Generated from YAML.
      - type: link
        text: Docs
        href: /docs/
        class: docs-link
      - kind: section
        props:
          label: Details
          class: details-section
        children:
          - kind: heading
            props:
              level: 3
              text: Details
          - kind: paragraph
            props:
              text: Everything is generated deterministically.
      - kind: div
        island: live_status
        props:
          class: live-status
        children:
          - kind: paragraph
            props:
              text: Island content
      - kind: button
        widget: save_button
        props:
          id: save-action
          text: Save
          class: primary-action
        actions:
          - name: save_click
            event: click
            client_js: "document.body.setAttribute('data-blueprint-clicked', 'yes'); G.updateText('[data-action-result]', 'Saved by browser');"
      - kind: paragraph
        props:
          text: Waiting
          data-action-result: true
      - kind: entity_list
        text: Latest posts
        entity: posts
        fields: [title, status]
        limit: 5
        empty_text: No posts yet.
endpoints:
  - name: publish_post
    method: POST
    path: /posts/{id}/publish
    entity: posts
    handler: publishPost
middleware:
  - request_logger
plugins:
  - name: analytics
helpers:
  - name: normalize_slug
`
}
