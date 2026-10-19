// Package buildoutput identifies generated framework output directories.
package buildoutput

import (
	"io/fs"
	"path"
)

// StaticExport identifies an exported site by its framework assets. A module
// directory remains source even when it also contains copied runtime assets.
func StaticExport(tree fs.FS, dir string) bool {
	if _, err := fs.Stat(tree, path.Join(dir, "go.mod")); err == nil {
		return false
	}
	for _, name := range []string{"__gofastr/runtime.js", "__gofastr/app.css"} {
		info, err := fs.Stat(tree, path.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
