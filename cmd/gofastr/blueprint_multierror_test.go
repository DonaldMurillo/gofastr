package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// multiErrorBlueprint carries three independent schema errors: a
// duplicate entity, a screen with no route, and an endpoint with no
// handler. One validation pass must report all three — the alternative
// is a serial guess-and-recompile loop where each fix reveals the next
// error.
func TestValidateReportsAllSchemaErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Multi
  module: example.com/multi
entities:
  - name: posts
    fields:
      - name: title
        type: string
  - name: posts
    fields:
      - name: body
        type: text
screens:
  - name: home
    route: /
  - name: about
endpoints:
  - name: broken
    method: GET
    path: /broken
`)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("blueprint with three schema errors validated")
	}
	msg := err.Error()
	for _, want := range []string{
		`duplicate entity "posts"`,              // entities section
		`screen "about" route is required`,      // screens section
		`endpoint "broken" handler is required`, // endpoints section
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("one-pass validation missed %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "3 validation errors") {
		t.Errorf("error does not say how many findings there are:\n%s", msg)
	}
}

// An unknown top-level key that is a known app-level setting must point
// at the right nesting level — `auth:` at the root silently does nothing
// (the real key is app.auth), which is exactly the misplacement an agent
// or a YAML-merging tool produces.
func TestUnknownKeySuggestsCorrectLocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		yml  string
		want string
	}{
		{"auth at root", "app:\n  name: A\nauth:\n  enabled: true\n", "app.auth"},
		{"theme at root", "app:\n  name: A\ntheme:\n  primary: \"#111111\"\n", "app.theme"},
		{"db at root", "app:\n  name: A\ndb:\n  driver: sqlite\n", "app.db"},
		{"screens under app", "app:\n  name: A\n  screens: []\n", "top level"},
		{"entities under app", "app:\n  name: A\n  entities: []\n", "top level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "gofastr.yml")
			writeTestFile(t, path, tc.yml)
			_, err := loadBlueprint(path)
			if err == nil {
				t.Fatalf("misplaced known key %s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not suggest the correct location %q:\n%s", tc.want, err.Error())
			}
		})
	}
}

// Every unknown key at one level must be reported in the same pass, not
// one per run (they are all in the same map — the fix is the same edit).
func TestUnknownKeysAllReportedAtOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, "app:\n  name: A\n  bogus_one: 1\n  bogus_two: 2\n")
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("two unknown app keys were accepted")
	}
	for _, want := range []string{`bogus_one`, `bogus_two`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("unknown key %q not reported in the same pass:\n%s", want, err.Error())
		}
	}
}
