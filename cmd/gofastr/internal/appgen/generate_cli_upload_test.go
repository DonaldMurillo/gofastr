package appgen

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A file flag joins the mutation verbs' flag set, so it gets the checks a
// field flag gets: a usable name, not a reserved one, not a duplicate.
func TestCLIUploadFlagChecks(t *testing.T) {
	for name, tc := range map[string]struct {
		fields []framework.FieldDeclaration
		want   string
	}{
		"unusable":  {[]framework.FieldDeclaration{{Name: "9logo", Type: "image"}}, "does not derive a usable CLI flag"},
		"reserved":  {[]framework.FieldDeclaration{{Name: "json", Type: "file"}}, "which is reserved"},
		"duplicate": {[]framework.FieldDeclaration{{Name: "status", Type: "string"}, {Name: "status_ne", Type: "image"}}, "both derive CLI flag --status-ne"},
	} {
		decls := []framework.EntityDeclaration{{Name: "customers", Table: "customers", Fields: tc.fields}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// The CLI offers a file flag for exactly the fields the client has an
// upload slot for: never a read-only, auto-generated or hidden one.
func TestCLIUploadFlagsMatchClient(t *testing.T) {
	decl := uploadDecl()
	decl.Fields = append(decl.Fields, framework.FieldDeclaration{Name: "stamp", Type: "file", AutoGenerate: "uuid"})
	spec, err := buildCLISpec([]framework.EntityDeclaration{decl}, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range spec.Entities[0].Uploads {
		got = append(got, f.Snake)
	}
	if want := []string{"logo", "contract"}; !slices.Equal(got, want) {
		t.Errorf("file flags = %v, want %v", got, want)
	}
}
