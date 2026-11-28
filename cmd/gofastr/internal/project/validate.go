package project

import (
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/ancestor"
)

// FindEnclosingGoMod returns the module path and directory of the nearest go.mod at or above dir.
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
