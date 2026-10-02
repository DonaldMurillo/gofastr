package pages

import (
	"example.com/shape-zoo/styles"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The constant reaches only a sheet-name sink, in another package.
func init() {
	registry.RegisterStyle(styles.ButtonSheet, func(style.Theme) string { return "" })
}
