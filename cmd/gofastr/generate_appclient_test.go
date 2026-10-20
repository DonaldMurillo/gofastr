package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// addPostsField appends a field to the posts declaration's schema in the
// generated entities/posts.go, the edit a developer makes by hand.
func addPostsField(t *testing.T, dir string) {
	t.Helper()
	p := filepath.Join(dir, "entities", "posts.go")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = `{Name: "title", Type: schema.String, Required: true}`
	if !strings.Contains(string(data), anchor) {
		t.Fatalf("posts.go has no %s anchor:\n%s", anchor, data)
	}
	updated := strings.Replace(string(data), anchor, anchor+`, {Name: "subtitle", Type: schema.String}`, 1)
	if err := os.WriteFile(p, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAppClientIsDerived(t *testing.T) {
	dir := cliTempModule(t)
	p := filepath.Join(dir, "entities", "client", "client.go")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	m, _, ok := codegen.ReadMarker(p, string(data))
	if !ok || m.Generator != "client" || m.Kind != codegen.KindDerived {
		t.Fatalf("marker = %+v (marked %v), want client derived", m, ok)
	}
	if !strings.Contains(string(data), "DO NOT EDIT.") {
		t.Fatal("no DO NOT EDIT line for go tooling")
	}
}

func TestPlainGenerateRebuildsClient(t *testing.T) {
	dir := cliTempModule(t)
	addPostsField(t, dir)
	out := covT_capStdout(t, func() { generateProject(nil) })
	data, _ := os.ReadFile(filepath.Join(dir, "entities", "client", "client.go"))
	if !strings.Contains(string(data), `json:"subtitle`) {
		t.Fatalf("client not rebuilt with the new field:\n%s", out)
	}
	if again := covT_capStdout(t, func() { generateProject(nil) }); strings.Contains(again, "Rebuilt") {
		t.Fatalf("rewrote an up-to-date client:\n%s", again)
	}
}

// legacyClient rewrites entities/client/client.go as the scaffold copy an
// older `generate app` wrote, plus an edit.
func legacyClient(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "entities", "client", "client.go")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := codegen.Stamp(p, codegen.StripMarker(p, string(data)), codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	stamped += "\n// my helper\n"
	if err := os.WriteFile(p, []byte(stamped), 0o600); err != nil {
		t.Fatal(err)
	}
	return stamped
}

func TestBuildSkipsEditedLegacyClient(t *testing.T) {
	dir := cliTempModule(t)
	before := legacyClient(t, dir)
	addPostsField(t, dir)
	out := covT_capStdout(t, func() { generateProject(nil) })
	after, _ := os.ReadFile(filepath.Join(dir, "entities", "client", "client.go"))
	if string(after) != before {
		t.Fatal("plain generate overwrote an edited scaffold client")
	}
	if !strings.Contains(out, "gofastr generate client --force") {
		t.Fatalf("no warning naming the takeover command:\n%s", out)
	}
}

func TestGenerateClientForceTakesOver(t *testing.T) {
	dir := cliTempModule(t)
	legacyClient(t, dir)
	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { runGenerateClient(nil) }) })
	if code != 1 || !strings.Contains(out, "--force") {
		t.Fatalf("want a refusal without --force, exit %d:\n%s", code, out)
	}
	covT_capStdout(t, func() { runGenerateClient([]string{"--force"}) })
	data, _ := os.ReadFile(filepath.Join(dir, "entities", "client", "client.go"))
	if m, _, ok := codegen.ReadMarker("client.go", string(data)); !ok || m.Generator != "client" {
		t.Fatalf("--force did not take the client over: %+v", m)
	}
}

func TestAddRebuildsClient(t *testing.T) {
	dir, bp := addSetup(t, addTestAppSpec("example.com/addtest"))
	covT_capStdout(t, func() { generateFromAppSpec(generateOptions{spec: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	if err := os.WriteFile(frag, []byte(addFragmentAppSpec()), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { generateFromAppSpec(generateOptions{spec: frag, add: true}) })
	client := readFile(t, dir, "entities/client/client.go")
	if !strings.Contains(client, "ListComments(") || !strings.Contains(client, "ListPosts(") {
		t.Fatalf("client after --add must cover old and new entities:\n%s", out)
	}
}

func TestSettingsGenerateRebuildsClient(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n")
	addPostsField(t, dir)
	out := covT_capStdout(t, func() { generateProject(nil) })
	data, _ := os.ReadFile(filepath.Join(dir, "entities", "client", "client.go"))
	if !strings.Contains(string(data), `json:"subtitle`) {
		t.Fatalf("client not rebuilt under a settings file:\n%s", out)
	}
}

func TestClientOnlyGenerateJSON(t *testing.T) {
	dir := cliTempModule(t)
	addPostsField(t, dir)
	out := covT_capStdout(t, func() { generateProject([]string{"--json"}) })
	if !strings.HasPrefix(strings.TrimSpace(out), "{") && !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("no JSON document:\n%s", out)
	}
	if !strings.Contains(out, "entities/client/client.go") {
		t.Fatalf("JSON does not name the rebuilt client:\n%s", out)
	}
}

func TestForceClientJSONIsClean(t *testing.T) {
	dir := cliTempModule(t)
	legacyClient(t, dir)
	out := covT_capStdout(t, func() { runGenerateClient([]string{"--force", "--json"}) })
	if s := strings.TrimSpace(out); !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		t.Fatalf("stdout is not just JSON:\n%s", out)
	}
}
