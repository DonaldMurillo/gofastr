package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// bpWithEnum builds a minimal one-entity blueprint whose status field
// carries the given enum value.
func bpWithEnum(v string) Blueprint {
	return Blueprint{Entities: []framework.EntityDeclaration{{
		Name: "tickets",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "string", Values: []string{v}},
		},
	}}}
}

// bpWithField builds the same shape with an attacker-shaped field name.
func bpWithField(name string) Blueprint {
	return Blueprint{Entities: []framework.EntityDeclaration{{
		Name:   "tickets",
		Fields: []framework.FieldDeclaration{{Name: name, Type: "string"}},
	}}}
}

// Property: no spec-derived string may terminate the Go literal it is
// emitted into.
//
// The e2e emitters used a backtick raw literal, which has NO escape
// mechanism — an enum value or field name containing a backtick closed
// it and the remainder became real Go source in the generated
// e2e_test.go. `format.Source` did not catch it because the injected
// code is valid Go, so it ran at `go test` time with nobody reading the
// generated file. The blueprint is normally developer-authored, but the
// documented workflow has an AGENT authoring gofastr.yml from
// natural-language requirements — enum values and field names are
// exactly what gets transcribed.
func TestBlueprintSpecCannotEscapeGoLiteral(t *testing.T) {
	if err := validateBlueprint(bpWithEnum("open`+PWN()+`")); err == nil {
		t.Error("backtick in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithEnum(`open"+PWN()+"`)); err == nil {
		t.Error("double quote in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithEnum("open\nfunc PWN()")); err == nil {
		t.Error("newline in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithField("title`+PWN()+`")); err == nil {
		t.Error("backtick in a field name was accepted")
	}
	// The ordinary spec still validates.
	if err := validateBlueprint(bpWithEnum("open")); err != nil {
		t.Errorf("legitimate enum value rejected: %v", err)
	}
}

// Property: a spec-derived font family must not escape the CSS string
// literal it is interpolated into. Edge-trimming quotes missed an
// interior quote, which closed `font-family: '…'` and appended
// attacker-chosen rules to the app-wide stylesheet.
func TestFontFamilyCannotInjectCSS(t *testing.T) {
	for _, in := range []string{
		`Evil'} body{background:url('https://attacker.test/exfil')}@font-face{font-family:'z`,
		`Inter"; } body { display:none } .x{`,
		"Inter\\3c/style\\3e",
	} {
		got := blueprintFontFamilyName(in)
		if strings.ContainsAny(got, `'"{}();\/<>:`) {
			t.Errorf("font family %q survived sanitisation as %q", in, got)
		}
	}
	// Real families are untouched.
	for in, want := range map[string]string{
		"Inter":                     "Inter",
		"'Playfair Display', serif": "Playfair Display",
		"IBM Plex Sans":             "IBM Plex Sans",
	} {
		if got := blueprintFontFamilyName(in); got != want {
			t.Errorf("blueprintFontFamilyName(%q) = %q, want %q", in, got, want)
		}
	}
}
