package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// D-35: an app spec with auth on and no login_form screen still points its
// gates, its Sign in button and the battery's failed-login redirect at
// /login, so the generator synthesizes a /login screen holding the
// login_form block (ui.AuthCard + ui.Form), the way a writable list gets
// its <list>/create. An app spec that already hosts a login form, or has
// auth off, gets none.
func TestAppSpecSynthesizesLoginScreenWhenAuthHasNone(t *testing.T) {
	const base = `
app:
  name: Shop
  module: github.com/example/shop
  auth:
    enabled: %AUTH%
    dev_mode: true
entities:
  - name: notes
    crud: true
    fields:
      - name: title
        type: string
        required: true
nav:
  - label: Dashboard
    href: /dashboard
screens:
%LOGIN%  - name: dashboard
    route: /dashboard
    layout: app
    title: Dashboard
    access:
      auth: %AUTH%
    body:
      - kind: entity_list
        entity: notes
        fields: [title]
`
	render := func(t *testing.T, auth, login string) map[string]string {
		t.Helper()
		src := strings.NewReplacer("%AUTH%", auth, "%LOGIN%", login).Replace(base)
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		clitest.WriteTestFile(t, path, src)
		bp, err := appspec.LoadAppSpec(path)
		if err != nil {
			t.Fatalf("LoadAppSpec: %v", err)
		}
		if err := appspec.ValidateAppSpec(bp); err != nil {
			t.Fatalf("ValidateAppSpec: %v", err)
		}
		out := map[string]string{}
		for _, f := range mustRenderAppSpecFiles(t, bp) {
			out[f.Name] = f.Content
		}
		return out
	}

	t.Run("auth with no login screen", func(t *testing.T) {
		out := render(t, "true", "")
		screen, ok := out["screen_login.go"]
		if !ok {
			t.Fatalf("no /login screen was synthesized; %d files rendered", len(out))
		}
		for _, want := range []string{`ui.AuthCard(`, `"/auth/login"`, `"/login"`} {
			if !strings.Contains(screen, want) {
				t.Errorf("synthesized login screen lacks %s:\n%s", want, screen)
			}
		}
		var all strings.Builder
		for _, c := range out {
			all.WriteString(c)
		}
		for _, want := range []string{`authPolicy("/login"`, `SetDefaultLoginErrorPath("/login")`, `Href: "/login"`} {
			if !strings.Contains(all.String(), want) {
				t.Errorf("the app does not reach the synthesized screen through %s", want)
			}
		}
	})
	t.Run("authored login screen", func(t *testing.T) {
		const signin = `  - name: signin
    route: /signin
    title: Sign in
    body:
      - kind: login_form
`
		if _, ok := render(t, "true", signin)["screen_login.go"]; ok {
			t.Error("a login screen was synthesized beside the authored one")
		}
	})
	t.Run("auth off", func(t *testing.T) {
		if _, ok := render(t, "false", "")["screen_login.go"]; ok {
			t.Error("a login screen was synthesized with auth off")
		}
	})
}

// The generated app pointed every form auth error at the login screen,
// and its pages send no Referer, so a failed signup landed on login. An
// app spec with a signup_form screen now also sets the battery's register
// error path to that screen; one without a signup form sets none.
func TestAppSpecSetsRegisterErrorPath(t *testing.T) {
	const base = `
app:
  name: Shop
  module: github.com/example/shop
  auth:
    enabled: true
    dev_mode: true
entities:
  - name: notes
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: signin
    route: /signin
    title: Sign in
    body:
      - kind: login_form
%SIGNUP%`
	render := func(t *testing.T, signup string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		clitest.WriteTestFile(t, path, strings.Replace(base, "%SIGNUP%", signup, 1))
		bp, err := appspec.LoadAppSpec(path)
		if err != nil {
			t.Fatalf("LoadAppSpec: %v", err)
		}
		if err := appspec.ValidateAppSpec(bp); err != nil {
			t.Fatalf("ValidateAppSpec: %v", err)
		}
		var all strings.Builder
		for _, f := range mustRenderAppSpecFiles(t, bp) {
			all.WriteString(f.Content)
		}
		return all.String()
	}
	const signup = `  - name: join
    route: /join
    title: Join
    body:
      - kind: section
        children:
          - kind: signup_form
`
	if out := render(t, signup); !strings.Contains(out, `auth.SetDefaultRegisterErrorPath("/join")`) {
		t.Error("an app spec with a signup form at /join does not set the register error path to it")
	}
	if out := render(t, ""); strings.Contains(out, "SetDefaultRegisterErrorPath") {
		t.Error("an app spec with no signup form sets a register error path")
	}
}
