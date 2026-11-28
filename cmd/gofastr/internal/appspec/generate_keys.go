package appspec

import (
	"slices"

	"github.com/DonaldMurillo/gofastr/framework"
)

// DeclKeyIsID reports whether the entity keys on the generated id alone.
func DeclKeyIsID(decl framework.EntityDeclaration) bool {
	return decl.PrimaryKey == nil || slices.Equal(decl.PrimaryKey, []string{"id"})
}
