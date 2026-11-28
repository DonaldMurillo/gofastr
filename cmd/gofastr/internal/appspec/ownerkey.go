package appspec

import (
	"fmt"
	"slices"
	"strings"

	framework "github.com/DonaldMurillo/gofastr/framework"
)

func ownerScopedKeyProblem(decl framework.EntityDeclaration) error {
	owner := EntityDeclarationScope(decl).OwnerField
	if owner == "" {
		return nil
	}
	if len(decl.PrimaryKey) == 0 {
		for _, f := range decl.Fields {
			if f.Name == "id" && f.AutoGenerate == "" {
				return fmt.Errorf("app spec: entity %q is owner-scoped (owner_field: %s) but declares an id field without auto_generate, so the client sets the key and every account shares it: a second user creating an id another user holds gets 409 for a row they cannot see, which also tells them it exists. Give id auto_generate: uuid, or drop the id field and let the framework generate it",
					decl.Name, owner)
			}
		}
		return nil
	}
	if slices.Contains(decl.PrimaryKey, owner) {
		return nil
	}
	for _, f := range decl.Fields {
		if f.AutoGenerate != "" && slices.Contains(decl.PrimaryKey, f.Name) {
			return nil
		}
	}
	key := strings.Join(decl.PrimaryKey, ", ")
	return fmt.Errorf("app spec: entity %q is owner-scoped (owner_field: %s) but its primary_key [%s] leaves %s out, so every account shares the key: a second user creating a key another user holds gets 409 for a row they cannot see, which also tells them it exists. Add %s to primary_key ([%s, %s]) and declare it as a field, since a key column must be one the API returns, or drop primary_key and declare a unique index over [%s, %s]",
		decl.Name, owner, key, owner, owner, owner, key, owner, key)
}
