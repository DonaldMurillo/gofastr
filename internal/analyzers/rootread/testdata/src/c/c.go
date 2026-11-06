// Package c pins the 2026-11 round's sink additions on the read side:
// os.ReadDir (the directory-listing twin of the os.Open arm — a
// symlinked directory component discloses whatever it points at) and
// fs.ReadDir (the package form the fs.ReadFile / fs.Stat arms already
// had), each with its fix posture beside it.
package c

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// listJobs lists a caller-named directory under a root.
func listJobs(workDir, user string) ([]os.DirEntry, error) {
	return os.ReadDir(filepath.Join(workDir, "jobs", user)) // want `read under a root with lexical containment only`
}

// listFixed resolves the chain before listing.
func listFixed(workDir, user string) ([]os.DirEntry, error) {
	p := filepath.Join(workDir, "jobs", user)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(real)
}

// fsListDir is the fs.ReadDir package form, in an HTTP-serving
// function on a caller-supplied fs.
func fsListDir(w http.ResponseWriter, r *http.Request, exports fs.FS, name string) {
	if _, err := fs.ReadDir(exports, name); err != nil { // want `read under a root with lexical containment only`
		http.NotFound(w, r)
		return
	}
}
