package project

import (
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/ancestor"
)

// FindEnclosingGoMod walks up from dir looking for a go.mod, mirroring how
// the go tool resolves the main module. Returns the declared module path and
// the directory containing go.mod, or empty strings when none is found.
func FindEnclosingGoMod(dir string) (modulePath, moduleRoot string) {
	moduleRoot, found, _ := ancestor.Find(dir, func(dir string) (bool, error) {
		modulePath = contracts.ReadModulePath(dir)
		return modulePath != "", nil
	})
	if !found {
		return "", ""
	}
	return modulePath, moduleRoot
}
