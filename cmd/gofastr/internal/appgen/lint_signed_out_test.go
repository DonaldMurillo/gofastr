package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// The shortest app spec there is, one entity and one list screen with no
// app.auth, loaded and generated without a word. Auto-CRUD needs a
// session to read the entity and the app has no way to sign in, so the
// generated list rendered "Not available" to every visitor, and the
// app's own axe_test.go failed on the heading-less page that left
// (page-has-heading-one on the list and on its create screen). validate
// and generate now say so, naming the screens and the two ways out.

func loadSignedOutSpec(t *testing.T, app, entity string) appspec.AppSpec {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	spec := "app:\n  name: Shop\n" + app + `entities:
  - name: packages
` + entity + `    fields:
      - name: name
        type: string
  - name: notes
    public: true
    fields:
      - name: body
        type: string
screens:
  - name: list
    route: /packages
    title: Packages
    body:
      - kind: entity_list
        entity: packages
        fields: [name]
        create: true
  - name: dash
    route: /
    title: Home
    body:
      - kind: section
        children:
          - kind: entity_list
            entity: packages
            fields: [name]
      - kind: entity_list
        entity: notes
        fields: [body]
`
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return bp
}

func TestSignedOutScreenWarns(t *testing.T) {
	got := LintSignedOutScreens(loadSignedOutSpec(t, "", ""))
	if len(got) != 1 {
		t.Fatalf("want one finding (packages; notes is public), got %d: %+v", len(got), got)
	}
	msg := got[0].Message()
	for _, want := range []string{`"packages"`, `"list"`, `"dash"`, "app.auth.enabled", "public: true"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %s:\n%s", want, msg)
		}
	}
}

// Each of these makes the entity readable, or is a gate the author chose:
// the lint stays quiet.
func TestSignedOutScreenQuiet(t *testing.T) {
	for name, tc := range map[string]struct{ app, entity string }{
		"auth on":       {app: "  auth:\n    enabled: true\n"},
		"public entity": {entity: "    public: true\n"},
		"access gate":   {entity: "    access:\n      create: packages:write\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := LintSignedOutScreens(loadSignedOutSpec(t, tc.app, tc.entity)); len(got) != 0 {
				t.Fatalf("want no finding, got %+v", got)
			}
		})
	}
}

// A fragment (generate app --add) has no app section of its own; the
// project it joins turns auth on, so its screens are not signed out.
func TestSignedOutScreenFragmentReadsProject(t *testing.T) {
	bp := loadSignedOutSpec(t, "", "")
	bp.App = appspec.AppSpecApp{} // a fragment carries no app section
	bp.SetProjectApp(appspec.AppSpecApp{Auth: appspec.AppSpecAuth{Enabled: true}})
	if got := LintSignedOutScreens(bp); len(got) != 0 {
		t.Fatalf("project has auth on, want no finding, got %+v", got)
	}
}
