package generatecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

func TestWatchTracksOwnedStyleInputs(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "site"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(directory, "gofastr.settings.yml")
	if err := os.WriteFile(config, []byte("version: 1\ncodegen:\n  generators:\n    - name: styles\n      config:\n        patterns: [./site/...]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	options := appgen.GenerateOptions{ConfigPath: config}
	previous := hashGenerateInputs(options)
	for _, name := range []string{"card.style.css", "brand.tokens.css"} {
		path := filepath.Join(directory, "site", name)
		for _, content := range []string{".a{color:inherit}", ".a{color:currentColor}"} {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			current := hashGenerateInputs(options)
			if current == previous {
				t.Fatalf("%s creation/edit did not change watch hash", name)
			}
			previous = current
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		current := hashGenerateInputs(options)
		if current == previous {
			t.Fatalf("%s removal did not change watch hash", name)
		}
		previous = current
	}
	if err := os.WriteFile(filepath.Join(directory, "outside.style.css"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hashGenerateInputs(options) != previous {
		t.Fatal("style outside configured patterns changed watch hash")
	}
}
