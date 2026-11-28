package packcmd

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func toKebabCase(s string) string {
	return strings.ReplaceAll(appspec.ToSnakeCase(s), "_", "-")
}
