package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestScaffoldNameGuardedByValidate: the `gofastr generate entity|screen
// <name>` argv is the same trust boundary the entity mode above defends —
// a name typed (or scripted) at the CLI becomes the app spec fragment's
// entity/screen name, which lands in identifier position and in the emitted
// file name (entities/<name>.go, screens via screenFileName). The scaffold
// path must inherit validateAppSpec's identifier guard rather than
// synthesizing a fragment that bypasses it: generateAppSpec runs the same
// validateAppSpec, so a literal-breaking or path-shaped name must be
// refused there. Pinned green: the guard exists; this keeps a future
// "scaffold skips validation for convenience" refactor from opening it.
func TestScaffoldNameGuardedByValidate(t *testing.T) {
	for _, name := range []string{
		`x"); PWN() //`, // breaks out of the emitted Go literal
		`../evil`,       // path traversal via the derived file name
		`a/b`,           // ditto, slash survives toCamelCase
		`2fa_tokens`,    // leading digit: no valid Go identifier
		"x`y",           // backtick: raw struct-tag literal
	} {
		bp := AppSpec{Entities: []framework.EntityDeclaration{{
			Name:   name,
			Fields: []framework.FieldDeclaration{{Name: "name", Type: "string", Required: true}},
		}}}
		err := ValidateAppSpec(bp)
		if err == nil {
			t.Errorf("scaffold entity name %q: accepted; it must be refused before entities/%s.go is emitted", name, name)
			continue
		}
		if !strings.Contains(err.Error(), "identifier") && !strings.Contains(err.Error(), name) {
			t.Errorf("scaffold entity name %q: error neither names the value nor says identifier: %v", name, err)
		}
	}
	// Control: the names the scaffold is FOR still pass.
	bp := AppSpec{Entities: []framework.EntityDeclaration{{
		Name:   "two_fa_tokens",
		Fields: []framework.FieldDeclaration{{Name: "name", Type: "string", Required: true}},
	}}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("plain scaffold name must validate: %v", err)
	}
}
