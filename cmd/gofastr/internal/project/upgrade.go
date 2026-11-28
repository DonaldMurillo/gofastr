package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/mod/modfile"
)

// GofastrModule is the framework's module path.
const GofastrModule = "github.com/DonaldMurillo/gofastr"

// GoModGofastrVersion returns the gofastr version root/go.mod requires and whether a replace overrides it.
func GoModGofastrVersion(root string) (version string, replaced bool, err error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false, fmt.Errorf("read go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", body, nil)
	if err != nil {
		return "", false, fmt.Errorf("parse go.mod: %s", textsafe.ScrubTerminal(err.Error()))
	}
	for _, r := range f.Require {
		if r.Mod.Path == GofastrModule {
			version = r.Mod.Version
		}
	}
	for _, r := range f.Replace {
		if r.Old.Path == GofastrModule {
			replaced = true
		}
	}
	if version == "" {
		return "", replaced, fmt.Errorf("go.mod does not require %s: is this a GoFastr app?", GofastrModule)
	}
	return version, replaced, nil
}
