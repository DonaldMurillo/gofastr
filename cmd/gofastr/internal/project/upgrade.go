package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"golang.org/x/mod/modfile"
)

// GofastrModule is the module path both go.mod inspection and the
// mechanical upgrade steps operate on.
const GofastrModule = "github.com/DonaldMurillo/gofastr"

// GoModGofastrVersion reads root/go.mod and returns the required
// gofastr version plus whether a replace directive overrides it (a
// local replace means the version in go.mod may not be what actually
// builds). It parses with golang.org/x/mod/modfile, the go command's
// own grammar, so only a require names the version: a gofastr line in
// an exclude, retract or replace block (single-line or block form) is
// never misread as one.
func GoModGofastrVersion(root string) (version string, replaced bool, err error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false, fmt.Errorf("read go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", body, nil)
	if err != nil {
		return "", false, fmt.Errorf("parse go.mod: %s", cli.ScrubTerminalOutput(err.Error()))
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
