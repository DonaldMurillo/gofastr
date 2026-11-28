package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEmptyDeclarationGroupsEmitNothing(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "notes", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		Scope: &fwentity.ScopeDeclaration{}, Exposure: &fwentity.ExposureDeclaration{ReadScope: &fwentity.ReadScopeDeclaration{}}}
	src, err := RenderEntityRegistration(decl)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"Scope:", "Exposure:", "ReadScope:"} {
		if strings.Contains(src, group) {
			t.Errorf("an all-zero group emitted %s, so pack would read back a group the author never wrote:\n%s", group, src)
		}
	}
}

func TestAddedEntitiesContinueTheOrder(t *testing.T) {
	files, err := renderGeneratedProjectWithOrder([]framework.EntityDeclaration{{Name: "notes", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == entityFileName("notes") {
			if !strings.Contains(f.Content, "registrar{order: 5,") {
				t.Fatalf("an entity added after five keeps order 5:\n%s", f.Content)
			}
			return
		}
	}
	t.Fatal("no entities/notes.go")
}

func TestJSONFieldIsAnyJSONValue(t *testing.T) {
	if got := goTypeForField("json"); got != "any" {
		t.Fatalf("goTypeForField(json) = %s, want any: an array or a number must decode", got)
	}
}

func TestDuplicateDeclsSeeStructFields(t *testing.T) {
	src := "package client\n\ntype Notes struct{ UnmarshalJSON int }\n\nfunc (Notes) UnmarshalJSON([]byte) error { return nil }\n"
	if err := refuseDuplicateDecls([]GeneratedFile{{Name: "client/client.go", Content: src}}); err == nil {
		t.Fatal("a field and a method of one name on one type must be refused")
	}
}
