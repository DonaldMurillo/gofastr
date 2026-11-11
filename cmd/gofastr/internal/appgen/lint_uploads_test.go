package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An image or file field gets an upload control and a multipart API, but
// the generated main.go wires no file storage, so every upload answered
// 400 "server has no file storage configured". validate and generate
// warn, naming the entity, the fields and the option main.go needs.
func TestUploadFieldWithoutStorageWarns(t *testing.T) {
	bp := appspec.AppSpec{Entities: []framework.EntityDeclaration{
		{Name: "customers", Fields: []framework.FieldDeclaration{
			{Name: "name", Type: "string"},
			{Name: "logo", Type: "image"},
			{Name: "contract", Type: " File "},
			// No client upload reaches a hidden or read-only field.
			{Name: "thumb", Type: "image", ReadOnly: true},
			{Name: "scan", Type: "file", Hidden: true},
		}},
		{Name: "notes", Fields: []framework.FieldDeclaration{{Name: "body", Type: "text"}}},
	}}
	got := LintUploadFields(bp)
	if len(got) != 1 {
		t.Fatalf("findings = %v, want one for customers", got)
	}
	msg := got[0].Message()
	for _, want := range []string{`"customers"`, "logo", "contract", "framework.WithFileStorage", "400"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "name,") || strings.Contains(msg, "notes") || strings.Contains(msg, "thumb") || strings.Contains(msg, "scan") {
		t.Errorf("message names a non-upload field or entity: %s", msg)
	}
	bp.Entities = []framework.EntityDeclaration{{Name: "avatars", Fields: []framework.FieldDeclaration{
		{Name: "thumb", Type: "image", ReadOnly: true},
	}}}
	if got := LintUploadFields(bp); len(got) != 0 {
		t.Errorf("entity with only a read-only image warned: %v", got)
	}
	bp.Entities = []framework.EntityDeclaration{{Name: "notes", Fields: []framework.FieldDeclaration{{Name: "body", Type: "text"}}}}
	if got := LintUploadFields(bp); len(got) != 0 {
		t.Errorf("spec without upload fields warned: %v", got)
	}
}
