package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// componentIDAppSpec has one screen with no description whose form has a
// relation field, so the generator gives it a ComponentID, a mount action
// for the select's options and no ScreenDescription.
const componentIDAppSpec = `app:
  name: Shop
  module: example.com/addtest
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: authors
    fields:
      - name: name
        type: string
  - name: posts
    fields:
      - name: title
        type: string
      - name: author_id
        type: relation
        to: authors
screens:
  - name: new_post
    route: /posts/new
    title: New post
    body:
      - type: entity_form
        entity: posts
`

// componentIDProbe runs inside the generated app: it serves the
// generated screen through uihost and checks that the page loads the
// widget script of the id the screen renders as data-component.
const componentIDProbe = `package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestComponentIDProbe(t *testing.T) {
	s := &NewPostScreen{}
	if _, ok := any(s).(app.ScreenDescriber); ok {
		t.Fatal("NewPostScreen has a ScreenDescription; the probe needs a screen without one")
	}
	a := app.NewApp("probe")
	a.RegisterScreen(app.NewScreen("/posts/new", s), nil)
	ds := uihost.New(a)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/posts/new", nil))
	body := w.Body.String()
	id := s.ComponentID()
	if !strings.Contains(body, "data-component=\"" + id + "\"") {
		t.Fatalf("page does not render data-component=%q:\n%s", id, body)
	}
	if tag := "src=\"/__gofastr/widget/" + id + ".js?v="; !strings.Contains(body, tag) {
		t.Fatalf("page renders data-component=%q but has no %s tag, so its relation selects load no options:\n%s", id, tag, body)
	}
	if !strings.Contains(ds.GetActionJS(), id) {
		t.Fatalf("no action compiled under %q:\n%s", id, ds.GetActionJS())
	}
}
`

// A generated screen with no description still compiles its actions under
// the ComponentID it renders as data-component, so its page gets the
// screen-<name>.js tag and the entity form's relation selects get options.
func TestUndescribedScreenGetsItsWidgetScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated app")
	}
	clitest.VariantBuild(t, "framework/uihost/actions.go", "cmd/gofastr/internal/appgen/appspec.go")
	dir, _ := generateBaseProject(t, componentIDAppSpec)
	screen := clitest.ReadFile(t, dir, "screen_new_post.go")
	if !strings.Contains(screen, ") ComponentID() string") || !strings.Contains(screen, ") Actions()") || strings.Contains(screen, "ScreenDescription(") {
		t.Fatalf("generated screen is not the undescribed, action-bearing shape this test needs:\n%s", screen)
	}
	if err := os.WriteFile(filepath.Join(dir, "component_id_probe_test.go"), []byte(componentIDProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "-run", "TestComponentIDProbe", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated undescribed screen lost its widget script: %v\n%s", err, out)
	}
}
