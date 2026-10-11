package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// createTheme returns the stock framework theme, unmodified. The site
// is the framework's own showcase, so it ships no palette, spacing or
// type overrides and no stylesheet of its own: every surface is a
// framework/ui component on the default tokens. The only app-side
// styling is the owned chrome sheets (siteheader, sitefooter, docpage),
// whose extra tokens main.go adds with Theme.Extend.
func createTheme() style.Theme {
	return theme.Default(theme.Overrides{})
}
