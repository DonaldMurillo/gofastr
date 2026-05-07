package main

import (
	"github.com/gofastr/gofastr/core-ui/style"
)

func createTheme() style.Theme {
	base := style.DefaultTheme()
	custom := style.Theme{
		Colors: style.Colors{
			"primary":   "#6366F1", // indigo
			"secondary": "#8B5CF6", // violet
		},
	}
	return style.MergeThemes(base, custom)
}
