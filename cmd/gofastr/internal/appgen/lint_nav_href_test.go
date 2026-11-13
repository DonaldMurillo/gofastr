package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestNavHrefServed(t *testing.T) {
	base := func() appspec.AppSpec {
		return appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", APIPrefix: "api"},
			Screens:  []appspec.AppSpecScreen{{Name: "home", Route: "/"}, {Name: "record", Route: "/posts/{id}"}, {Name: "cafe", Route: "/café"}, {Name: "docs", Route: "/docs/{path...}"}, {Name: "typed", Route: "/typed/{id:int}"}, {Name: "colon", Route: "/colon/:slug"}},
			Entities: []framework.EntityDeclaration{{Name: "tickets", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
		}
	}
	for _, tc := range []struct {
		name  string
		items []appspec.AppSpecNavItem
		want  string
	}{
		{"top", []appspec.AppSpecNavItem{{Href: "/nowhere"}}, "nav[0].href"},
		{"nested", []appspec.AppSpecNavItem{{Items: []appspec.AppSpecNavItem{{Href: "/nowhere"}}}}, "nav[0].items[0].href"},
		{"role_without_auth", []appspec.AppSpecNavItem{{Href: "/nowhere", Role: "admin"}}, "nav[0].href"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bp := base()
			bp.Nav = tc.items
			if findings := LintNavHrefs(bp, ""); len(findings) != 1 || !strings.Contains(findings[0].Message(), tc.want) || !strings.Contains(findings[0].Message(), "strict boot refuses") {
				t.Fatalf("lint = %v; want unserved link at %s", findings, tc.want)
			}
		})
	}
	for _, href := range []string{"/", "/posts/42?tab=all#top", "/docs/a/b", "/typed/42", "/colon/hello", "/caf%C3%A9", "/robots.txt", "/mcp", "/api/docs", "/healthz", "/readyz", "/api/llm.md", "/api/tickets/_export.csv", "/__gofastr/runtime.js", "/api/tickets", "/api/tickets/42", "https://example.com", "//example.com", "#top", "mailto:a@example.com", "/posts/{id}"} {
		bp := base()
		bp.Nav = []appspec.AppSpecNavItem{{Href: href}}
		if findings := LintNavHrefs(bp, ""); len(findings) != 0 {
			t.Errorf("served/skipped href %q: %v", href, findings)
		}
	}
	bp := base()
	bp.App.Auth.Enabled, bp.App.Auth.DevMode = true, true
	bp.App.Auth.BasePath = "/account"
	bp.App.Admin.Enabled, bp.App.Admin.Path = true, "/staff"
	bp.Nav = []appspec.AppSpecNavItem{{Href: "/login"}, {Href: "/account/me"}, {Href: "/account/logout"}, {Href: "/staff/entities/tickets"}, {Href: "/nowhere", Role: "admin", Items: []appspec.AppSpecNavItem{{Href: "/also-nowhere"}}}}
	if findings := LintNavHrefs(bp, ""); len(findings) != 0 {
		t.Fatalf("battery routes and anonymous role filtering: %v", findings)
	}
	bp = base()
	bp.Screens = append(bp.Screens, appspec.AppSpecScreen{Name: "tickets", Route: "/work", Body: []appspec.AppSpecBlock{{Kind: "entity_list", Entity: "tickets", Fields: []string{"title"}, Create: true}}})
	bp.Nav = []appspec.AppSpecNavItem{{Href: "/work/create"}, {Href: "/api/tickets/42/history"}}
	bp.Endpoints = []settings.Endpoint{{Name: "history", Entity: "tickets", Method: "GET", Path: "{id}/history", Handler: "entities/tickets_endpoints.go#History"}}
	if findings := LintNavHrefs(bp, ""); len(findings) != 0 {
		t.Fatalf("generated create route and endpoint: %v", findings)
	}
	for _, href := range []string{"/typed/no", "/api/tickets/42/missing", "/account/me", "/mcp/missing", "/.well-known/nope", "/__gofastr/nope", "/robots.txt/"} {
		bp = base()
		bp.Nav = []appspec.AppSpecNavItem{{Href: href}}
		if findings := LintNavHrefs(bp, ""); len(findings) != 1 {
			t.Errorf("unserved href %q passed", href)
		}
	}
}

func TestNavHrefInputs(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", LocalePrefixes: true, StaticDir: "public"}, Screens: []appspec.AppSpecScreen{{Name: "about", Route: "/about/"}}}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "public"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public", "terms.pdf"), []byte("terms"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, href := range []string{"/about", "/about/", "/en/about", "/pt-br/about/", "/terms.pdf", "/en/terms.pdf"} {
		bp.Nav = []appspec.AppSpecNavItem{{Href: href}}
		if findings := LintNavHrefs(bp, dir); len(findings) != 0 {
			t.Errorf("served href %q warned: %v", href, findings)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "public", ".env"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	bp.Nav = []appspec.AppSpecNavItem{{Href: "/.env"}}
	if findings := LintNavHrefs(bp, dir); len(findings) != 1 {
		t.Fatalf("runtime refuses hidden static files: %v", findings)
	}
	bp.Nav = []appspec.AppSpecNavItem{{Href: "/terms.pdf"}}
	if findings := LintNavHrefs(bp, ""); len(findings) != 1 {
		t.Fatalf("unknown static root must not invent served files: %v", findings)
	}
	bp.App.PWA.Enabled = true
	for _, href := range []string{"/icons/icon-192.png", "/icons/icon-512.png", "/icons/icon-maskable.png", "/manifest.webmanifest", "/__gofastr/pwa/offline"} {
		bp.Nav = []appspec.AppSpecNavItem{{Href: href}}
		if findings := LintNavHrefs(bp, ""); len(findings) != 0 {
			t.Errorf("PWA href %q warned: %v", href, findings)
		}
	}
	bp.App.Admin.Enabled = true
	bp.App.Admin.Path = "/staff"
	bp.Nav = []appspec.AppSpecNavItem{{Href: "/staff/nowhere"}}
	if findings := LintNavHrefs(bp, ""); len(findings) != 1 {
		t.Errorf("admin's unserved child must warn: %v", findings)
	}
}

// generate reads the static dir under the directory the app is written
// to, as validate reads it beside the spec, not under the shell's cwd.
func TestNavHrefStaticUnderOutDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	spec := `
app:
  name: Demo
  module: example.com/demo
  static_dir: public
screens:
  - name: home
    route: /
nav:
  - label: Brochure
    href: /brochure.pdf
`
	if err := os.WriteFile("gofastr.yml", []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("gen", "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("gen", "public", "brochure.pdf"), []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := clitest.CaptureStdout(t, func() {
		GenerateFromAppSpec(GenerateOptions{Spec: "gofastr.yml", outputDir: "gen", outputSet: true, dryRun: true})
	})
	if strings.Contains(out, "nav[0].href") {
		t.Fatalf("a nav link to a file under gen/public warned:\n%s", out)
	}
}
