package appspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestLoadAppSpecDecodesCodegenSurface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(path, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
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
	if len(bp.Endpoints) != 1 || bp.Endpoints[0].Handler != "entities/posts_endpoints.go#PublishPost" {
		t.Fatalf("endpoints = %#v", bp.Endpoints)
	}
	if len(bp.Middleware) != 1 || len(bp.Plugins) != 1 || len(bp.Helpers) != 1 {
		t.Fatalf("stubs not decoded: middleware=%d plugins=%d helpers=%d", len(bp.Middleware), len(bp.Plugins), len(bp.Helpers))
	}
}

func TestAppSpecRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte("wat: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "wat"`) {
		t.Fatalf("loadAppSpec err = %v", err)
	}
}

func TestLoadAppSpecSupportsJSONInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.json")
	if err := os.WriteFile(path, []byte(`{"app":{"name":"Demo"},"entities":[{"name":"posts","fields":[{"name":"title","type":"string"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if bp.App.Name != "Demo" || len(bp.Entities) != 1 || bp.Entities[0].Name != "posts" {
		t.Fatalf("decoded app spec = %#v", bp)
	}
}

func TestLoadAppSpecMergesDirectoryInStableOrder(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "01-app.yml"), `
app:
  name: DirectoryDemo
`)
	clitest.WriteTestFile(t, filepath.Join(dir, "02-entities.yml"), `
entities:
  - name: users
    fields:
      - name: email
        type: string
`)
	clitest.WriteTestFile(t, filepath.Join(dir, "03-screens.yml"), `
screens:
  - name: home
    route: /
    title: Home
`)
	bp, err := LoadAppSpec(dir)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if bp.App.Name != "DirectoryDemo" || len(bp.Entities) != 1 || len(bp.Screens) != 1 {
		t.Fatalf("merged app spec = %#v", bp)
	}
}

func TestLoadAppSpecDirectoryValidatesAfterMerge(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "01-users.yml"), `
entities:
  - name: users
    fields:
      - name: email
        type: string
`)
	clitest.WriteTestFile(t, filepath.Join(dir, "02-posts.yml"), `
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
	bp, err := LoadAppSpec(dir)
	if err != nil {
		t.Fatalf("loadAppSpec split directory: %v", err)
	}
	if len(bp.Entities) != 2 {
		t.Fatalf("entities = %#v", bp.Entities)
	}
}

func TestLoadAppSpecRejectsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "README.md"), "no app specs here\n")
	_, err := LoadAppSpec(dir)
	if err == nil || !strings.Contains(err.Error(), "does not contain any app spec files") {
		t.Fatalf("loadAppSpec err = %v", err)
	}
}

func TestAppSpecValidationFailures(t *testing.T) {
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
			want: `unsupported token "not-a-token"`,
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
			// "2fa_tokens" camel-cases to "2faTokens", which is not a valid
			// Go identifier; the generated package would not compile.
			name: "bad entity identifier",
			yml: `
entities:
  - name: 2fa_tokens
    fields: []
`,
			want: `entity "2fa_tokens" does not produce a valid Go identifier`,
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
			name: "bulk only on entity list",
			yml: `
screens:
  - name: home
    route: /
    body:
      - kind: heading
        text: Hi
        bulk: true
`,
			want: `bulk applies only to an entity_list block`,
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
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish
    entity: posts
    method: WAT
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#Publish
`,
			want: `method "WAT" must be one of`,
		},
		{
			name: "missing handler",
			yml: `
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish
    entity: posts
    method: POST
    path: "{id}/publish"
`,
			want: `handler is required`,
		},
		{
			name: "bad endpoint entity",
			yml: `
endpoints:
  - name: publish
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#Publish
`,
			want: `hangs under entity "posts", which the app spec does not declare`,
		},
		{
			name: "duplicate endpoint route",
			yml: `
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#Publish
  - name: publish_again
    entity: posts
    method: POST
    path: "{post}/publish"
    handler: entities/posts_endpoints.go#PublishAgain
`,
			want: `is endpoint publish's route already`,
		},
		{
			name: "endpoint absolute path",
			yml: `
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish
    entity: posts
    method: POST
    path: /posts/{id}/publish
    handler: entities/posts_endpoints.go#Publish
`,
			want: `must be relative to the entity's route`,
		},
		{
			name: "endpoints share a handler",
			yml: `
entities:
  - name: posts
    fields:
      - name: title
        type: string
endpoints:
  - name: publish
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#Publish
  - name: archive
    entity: posts
    method: POST
    path: "{id}/archive"
    handler: entities/posts_endpoints.go#Publish
`,
			want: `both link entities/posts_endpoints.go#Publish`,
		},
		{
			name: "endpoints under an entity",
			yml: `
entities:
  - name: posts
    fields:
      - name: title
        type: string
    endpoints:
      - name: publish
        method: POST
        path: /publish
`,
			want: `an entity no longer lists endpoints`,
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
			clitest.WriteTestFile(t, path, tc.yml)
			_, err := LoadAppSpec(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadAppSpec err = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestAuthDevModeRejectsFuzzyBool(t *testing.T) {
	// YAML-1.1 muscle memory: `yes` would coerce to false via boolValue,
	// silently flipping the safe default into prod cookie mode on plain
	// HTTP AND suppressing the dev-mode warning. Must be a hard error.
	_, err := covT_decode(t, "app:\n  name: D\n  auth:\n    enabled: true\n    dev_mode: yes\n")
	if err == nil {
		t.Fatal("dev_mode: yes must be rejected, not coerced")
	}
	if !strings.Contains(err.Error(), "dev_mode") || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("error should name dev_mode and the remedy, got: %v", err)
	}
}

func TestAuthDevModeDefaultsTrue(t *testing.T) {
	// dev_mode omitted → true: a fresh generated app serves plain HTTP,
	// where the production cookie defaults (__Host-session + Secure)
	// never round-trip, so login would silently break out of the box.
	bp, err := covT_decode(t, "app:\n  name: D\n  auth:\n    enabled: true\n")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bp.App.Auth.DevMode {
		t.Fatal("auth.dev_mode omitted should default to true")
	}
}
