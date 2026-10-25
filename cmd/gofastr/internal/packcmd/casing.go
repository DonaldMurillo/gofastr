package packcmd

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// toKebabCase converts a name to kebab-case. It is toSnakeCase with
// hyphens instead of underscores, so the two share one boundary rule.
func toKebabCase(s string) string {
	return strings.ReplaceAll(appspec.ToSnakeCase(s), "_", "-")
}
