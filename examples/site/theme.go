package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// createTheme returns the stock framework theme. The site is the
// framework's own showcase, so it ships no palette, spacing or type
// overrides and no stylesheet of its own: every surface is a
// framework/ui component on the default tokens. The only app-side
// styling is the owned chrome sheets (siteheader, sitefooter, docpage),
// whose extra tokens main.go adds with Theme.Extend.
//
// The one knob is the documented one for a sticky header: a section
// reached by its #anchor (a breadcrumb, a rail) lands below the
// header instead of under it.
func createTheme() style.Theme {
	t := theme.Default(theme.Overrides{})
	t.Knobs = map[string]string{
		"ui-section-scroll-margin": "calc(var(--size-header-height) + var(--spacing-lg))",
	}
	return t
}
