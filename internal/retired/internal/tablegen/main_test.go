package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/retired"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// tableFile is the checked-in table, relative to this package.
const tableFile = "../../retired_table.gen.go"

// The checked-in table must be what the generator writes from the
// registry this binary embeds. This package imports internal/upgrade,
// so editing any release note (releases/*.yml, registry.yml) selects
// this test; regenerating the table then changes internal/retired and
// selects everything that renders through the retired-markup check.
//
//gofastr:test-reads /internal/retired/retired_table.gen.go
func TestTableIsCurrent(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load migration registry: %v", err)
	}
	want, err := render(reg)
	if err != nil {
		t.Fatalf("render table: %v", err)
	}
	got, err := os.ReadFile(filepath.FromSlash(tableFile))
	if err != nil {
		t.Fatalf("read the checked-in table: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("internal/retired/retired_table.gen.go does not match internal/upgrade's migration registry "+
			"(a release note retired or renamed a kit class or attribute); regenerate it from the repo root:\n\n\t%s\n", regenerate)
	}
}

const fixtureRegistry = `
through: v9.9.9
releases:
  - version: v9.0.0
    title: classes go
    notes:
      - change: "the ui-button class is gone"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [ui-button]
            attrs: [data-fui-signal, data-fui-toggle-]
          css:
            classes: [ui-hero]
      - change: "a non-breaking note adds nothing"
        find:
          strings:
            classes: [ui-kept]
  - version: v9.1.0
    title: more
    notes:
      - change: "the ui-form family is renamed"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [ui-form, ui-button]
            attrs: [data-placeholder]
`

// Breaking notes contribute their strings.classes, css.classes and
// strings.attrs in registry order; a non-breaking note contributes
// nothing; the table keeps what retired.New keeps.
func TestRenderKeepsBreakingKitNames(t *testing.T) {
	reg, err := upgrade.Parse(fixtureRegistry)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	var got []string
	for _, e := range candidates(reg) {
		got = append(got, e.Note.Version+" "+e.Kind+" "+e.Name)
	}
	want := []string{
		"v9.0.0 class ui-button", "v9.0.0 class ui-hero",
		"v9.0.0 attr data-fui-signal", "v9.0.0 attr data-fui-toggle-",
		"v9.1.0 class ui-form", "v9.1.0 class ui-button", "v9.1.0 attr data-placeholder",
	}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %q, want %q", got, want)
	}

	src, err := render(reg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, line := range []string{
		`{Version: "v9.0.0", Change: "the ui-button class is gone"},`,
		`{Version: "v9.1.0", Change: "the ui-form family is renamed"},`,
		`{Kind: "class", Name: "ui-button", Note: &tableNotes[0]},`,
		`{Kind: "class", Name: "ui-hero", Note: &tableNotes[0]},`,
		`{Kind: "attr", Name: "data-fui-toggle-", Note: &tableNotes[0]},`,
		`{Kind: "class", Name: "ui-form", Note: &tableNotes[1]},`,
	} {
		if !bytes.Contains(src, []byte(line)) {
			t.Errorf("rendered table lacks %s\n%s", line, src)
		}
	}
	for _, absent := range []string{"ui-kept", "data-placeholder", "a non-breaking note"} {
		if bytes.Contains(src, []byte(absent)) {
			t.Errorf("rendered table carries %q, which no runtime check reads\n%s", absent, src)
		}
	}
	if n := bytes.Count(src, []byte(`Name: "ui-button"`)); n != 1 {
		t.Errorf("ui-button appears %d times; a later mention must not add a second entry", n)
	}
}

// The generated table builds the same set the registry does: the check
// that serving code runs is the check the registry describes.
func TestTableMatchesRegistrySet(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load migration registry: %v", err)
	}
	want := retired.New(candidates(reg)).Entries()
	if len(want) == 0 {
		t.Fatal("the registry retires no kit name: the walk is broken, not the registry empty")
	}
	for _, e := range want {
		probe := `<i class="` + e.Name + `">`
		if e.Kind == "attr" {
			probe = `<i ` + e.Name + `>`
		}
		if len(retired.Current().Check([]byte(probe))) == 0 {
			t.Errorf("the registry retires %s %q but the shipped set does not flag it", e.Kind, e.Name)
		}
	}
}
