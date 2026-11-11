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
	if strings.Contains(msg, "name,") || strings.Contains(msg, "notes") {
		t.Errorf("message names a non-upload field or entity: %s", msg)
	}
	bp.Entities = bp.Entities[1:]
	if got := LintUploadFields(bp); len(got) != 0 {
		t.Errorf("spec without upload fields warned: %v", got)
	}
}
