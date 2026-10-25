package project

import (
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// FindEnclosingGoMod walks up from dir looking for a go.mod, mirroring how
// the go tool resolves the main module. Returns the declared module path and
// the directory containing go.mod, or empty strings when none is found.
func FindEnclosingGoMod(dir string) (modulePath, moduleRoot string) {
	for {
		if mod := contracts.ReadModulePath(dir); mod != "" {
			return mod, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}
