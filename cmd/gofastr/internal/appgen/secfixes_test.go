package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// F1: OwnerField must be emitted by the codegen text emitter.
func TestEmitOwnerField(t *testing.T) {
	reg, err := RenderEntityRegistration(framework.EntityDeclaration{Pagination: &framework.PaginationDeclaration{}, Exposure: &framework.ExposureDeclaration{}, Name: "notes", Scope: &framework.ScopeDeclaration{OwnerField: "user_id"}, Fields: []framework.FieldDeclaration{
		{Name: "title", Type: "string"},
	},
	})
	if err != nil {
		t.Fatalf("renderEntityRegistration: %v", err)
	}
	if !strings.Contains(reg, `OwnerField: "user_id"`) {
		t.Fatalf("generated registration missing OwnerField:\n%s", reg)
	}
}
