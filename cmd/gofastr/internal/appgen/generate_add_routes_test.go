package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// addMountsBase turns on every app setting that mounts a framework route:
// admin at a custom path, PWA, llm.md, auth at a custom base, locale
// prefixes and an API prefix. A fragment carries none of these, so its
// validation has to read them off the project it joins.
const addMountsBase = `app:
  name: Mounts
  module: example.com/addtest
  api_prefix: v1
  locale_prefixes: true
  llm_md: true
  pwa:
    enabled: true
  db:
    driver: sqlite
    url: file:test.db
  auth:
    enabled: true
    dev_mode: true
    base_path: /account
  admin:
    enabled: true
    path: /backoffice
    seed_email: admin@example.com
    seed_password: secret123
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: about
    route: /about
    title: About
`

// A fragment screen at a route the project's framework mounts (admin, PWA,
// llm.md, auth, the entity API) never renders: --add must refuse it the way
// validating the whole spec does, judging the project's settings, not the
// fragment's empty app.
func TestAddRefusesProjectMountRoutes(t *testing.T) {
	dir, bp := clitest.AddSetup(t, addMountsBase)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	add := func(route string) (int, string) {
		t.Helper()
		yml := "screens:\n  - name: probe\n    route: " + route + "\n    title: Probe\n"
		if err := os.WriteFile(frag, []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		var code int
		out := clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() {
				GenerateFromAppSpec(GenerateOptions{Spec: frag, add: true})
			})
		})
		return code, out
	}
	for _, tc := range []struct{ route, want string }{
		{"/backoffice", "admin back-office"},
		{"/backoffice/e/posts", "admin back-office"},
		{"/manifest.webmanifest", "PWA manifest"},
		{"/service-worker.js", "PWA service worker"},
		{"/llm-pages.md", "llm.md page index"},
		{"/guide/llm.md", "llm.md document"},
		{"/account/me", "auth battery"},
		{"/v1/posts", "CRUD API"},
	} {
		code, out := add(tc.route)
		if code != 1 {
			t.Errorf("route %s: want exit 1, got %d\n%s", tc.route, code, out)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("route %s: refusal does not name %q:\n%s", tc.route, tc.want, out)
		}
		if clitest.FileExists(dir, "screen_probe.go") {
			t.Fatalf("route %s: screen_probe.go written despite the refusal", tc.route)
		}
	}
	// The default /admin path is free in this project (-1: no exit).
	if code, out := add("/admin"); code != -1 || !clitest.FileExists(dir, "screen_probe.go") {
		t.Fatalf("/admin is free when admin sits at /backoffice; got exit %d:\n%s", code, out)
	}
}

// With locale prefixes on in the project, a fragment screen whose first
// segment reads as a locale tag gets the same warning a full generate gives.
func TestAddWarnsLocaleShapedRoute(t *testing.T) {
	dir, bp := clitest.AddSetup(t, addMountsBase)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	yml := "screens:\n  - name: spanish\n    route: /es/hola\n    title: Hola\n"
	if err := os.WriteFile(frag, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: frag, add: true})
	})
	if !strings.Contains(out, `"/es/hola" starts with "es"`) {
		t.Fatalf("no locale-route warning for /es/hola:\n%s", out)
	}
	// The project's own settings were judged when it was generated: the
	// fragment's run warns only about the fragment.
	if strings.Contains(out, "app.api_prefix") || strings.Contains(out, "app.admin.path") {
		t.Errorf("fragment run re-warned the project's settings:\n%s", out)
	}
}
