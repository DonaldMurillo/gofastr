package appgen

import (
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// LintOrder selects the position of module collision warnings.
type LintOrder uint8

const ModuleCollisionsFirst LintOrder = 1

// CommonLintFindings collects the shared warnings, with module collisions last by default.
func CommonLintFindings(bp appspec.AppSpec, navRoot string, order ...LintOrder) []string {
	collisions := messages(LintModuleCollision(bp))
	var out []string
	out = append(out, messages(LintOwnerScopedUnique(bp))...)
	out = append(out, messages(LintSignedOutScreens(bp))...)
	out = append(out, messages(LintPostFormsOnScreens(bp))...)
	out = append(out, messages(LintUploadFields(bp))...)
	out = append(out, messages(LintNavHrefs(bp, navRoot))...)
	out = append(out, messages(LintLocaleRoutes(bp))...)
	if slices.Contains(order, ModuleCollisionsFirst) {
		return slices.Insert(out, 0, collisions...)
	}
	return append(out, collisions...)
}

func messages[F interface{ Message() string }](findings []F) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Message()
	}
	return out
}
