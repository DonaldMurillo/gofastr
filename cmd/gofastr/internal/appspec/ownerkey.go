package appspec

import (
	"fmt"
	"slices"
	"strings"

	framework "github.com/DonaldMurillo/gofastr/framework"
)

// ownerScopedKeyProblem refuses an owner-scoped entity whose declared
// primary_key leaves the owner column out. Every account's rows share
// that key, so a second user creating a key another user holds gets
// 409 for a row the scope hides from them, and the 409 tells them the
// key exists. A key holding the owner keeps each user's rows apart, and
// so does a key column the server generates (auto_generate), which no
// caller can collide on.
//
// A unique field that leaves the owner out only warns
// (LintOwnerScopedUnique): a value unique across accounts, such as a
// public handle, can be deliberate. The key is the row's address, and
// an owner-scoped join keyed on the rows it joins means per-user rows;
// an author who wants the global value declares it as a unique field
// (or index) beside the generated id, where the warning names it.
func ownerScopedKeyProblem(decl framework.EntityDeclaration) error {
	owner := EntityDeclarationScope(decl).OwnerField
	if owner == "" || len(decl.PrimaryKey) == 0 || slices.Contains(decl.PrimaryKey, owner) {
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
