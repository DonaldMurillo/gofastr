package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

func WriteA11yTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const BadScreen = `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Image(html.ImageConfig{Src: "/x.png"})
}
`
