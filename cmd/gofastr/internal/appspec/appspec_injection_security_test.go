package appspec

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// bpWithEnum builds a minimal one-entity app spec whose status field
// carries the given enum value.
func bpWithEnum(v string) AppSpec {
	return AppSpec{Entities: []framework.EntityDeclaration{{
		Name: "tickets",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "string", Values: []string{v}},
		},
	}}}
}

// bpWithField builds the same shape with an attacker-shaped field name.
func bpWithField(name string) AppSpec {
	return AppSpec{Entities: []framework.EntityDeclaration{{
		Name:   "tickets",
		Fields: []framework.FieldDeclaration{{Name: name, Type: "string"}},
	}}}
}

// Property: no spec-derived string may terminate the Go literal it is
// emitted into.
//
// The e2e emitters used a backtick raw literal, which has NO escape
// mechanism: an enum value or field name containing a backtick closed
// it and the remainder became real Go source in the generated
// e2e_test.go. `format.Source` did not catch it because the injected
// code is valid Go, so it ran at `go test` time with nobody reading the
// generated file. The app spec is normally developer-authored, but the
// documented workflow has an AGENT authoring gofastr.yml from
// natural-language requirements: enum values and field names are
// exactly what gets transcribed.
func TestAppSpecSpecCannotEscapeGoLiteral(t *testing.T) {
	if err := ValidateAppSpec(bpWithEnum("open`+PWN()+`")); err == nil {
		t.Error("backtick in an enum value was accepted")
	}
	if err := ValidateAppSpec(bpWithEnum(`open"+PWN()+"`)); err == nil {
		t.Error("double quote in an enum value was accepted")
	}
	if err := ValidateAppSpec(bpWithEnum("open\nfunc PWN()")); err == nil {
		t.Error("newline in an enum value was accepted")
	}
	if err := ValidateAppSpec(bpWithField("title`+PWN()+`")); err == nil {
		t.Error("backtick in a field name was accepted")
	}
	// The ordinary spec still validates.
	if err := ValidateAppSpec(bpWithEnum("open")); err != nil {
		t.Errorf("legitimate enum value rejected: %v", err)
	}
}
