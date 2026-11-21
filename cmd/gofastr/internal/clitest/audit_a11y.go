package clitest

import "testing"

// WriteA11yTree lays down a throwaway tree of .ui.go fixtures.
func WriteA11yTree(t *testing.T, files map[string]string) string {
	t.Helper()
	return writeTree(t, files)
}

const BadScreen = `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Image(html.ImageConfig{Src: "/x.png"})
}
`
