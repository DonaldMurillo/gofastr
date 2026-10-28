// Package g pins bug bash 2026-10-27 I13 on the read side: a call
// whose name contains "symlink" gated every read in the function,
// whatever its body did and whatever path it was handed
// (pathflow.go:633-655 CallsSymlinkNamed). A guard counts only when
// its body really resolves or refuses symlinks and its argument is the
// read's own path, its Dir, or a local bound to either.
package g

import (
	"errors"
	"gsafe"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

type shelf struct{ rootDir string }

func checkSymlink(string) error { return nil }

func noSymlinkLeaf(p string) error {
	fi, err := os.Lstat(p)
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink")
	}
	return nil
}

// fakeGuard: a name, no body.
func (s *shelf) fakeGuard(ref string) ([]byte, error) {
	p := filepath.Join(s.rootDir, ref)
	if err := checkSymlink(p); err != nil {
		return nil, err
	}
	return os.ReadFile(p) // want `read under a root with lexical containment only`
}

// guardOther: a real guard on another path.
func (s *shelf) guardOther(ref string) ([]byte, error) {
	if err := noSymlinkLeaf(s.rootDir); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.rootDir, ref)) // want `read under a root with lexical containment only`
}

// guardOnPath: a real guard on the read's own leaf. Quiet.
func (s *shelf) guardOnPath(ref string) ([]byte, error) {
	p := filepath.Join(s.rootDir, ref)
	if err := noSymlinkLeaf(p); err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// fsFakeGuard: the fs.FS sink took the same function-wide name gate.
func fsFakeGuard(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	name := r.URL.Path
	_ = checkSymlink(name)
	f, err := fsys.Open(name) // want `read under a root with lexical containment only`
	if err == nil {
		f.Close()
	}
}

// guardCrossPackage: a real guard in another package, judged by its
// exported fact. Quiet.
func (s *shelf) guardCrossPackage(ref string) ([]byte, error) {
	p := filepath.Join(s.rootDir, ref)
	if err := gsafe.EnsureNoSymlinkPath(p); err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// guardFakeCrossPackage: named for the posture in another package,
// does none of it.
func (s *shelf) guardFakeCrossPackage(ref string) ([]byte, error) {
	p := filepath.Join(s.rootDir, ref)
	if err := gsafe.NoSymlinkFake(p); err != nil {
		return nil, err
	}
	return os.ReadFile(p) // want `read under a root with lexical containment only`
}

// lstatSymlinkExists Lstats but never consults ModeSymlink.
func lstatSymlinkExists(p string) error {
	_, err := os.Lstat(p)
	return err
}

func (s *shelf) lstatOnly(ref string) ([]byte, error) {
	p := filepath.Join(s.rootDir, ref)
	if err := lstatSymlinkExists(p); err != nil {
		return nil, err
	}
	return os.ReadFile(p) // want `read under a root with lexical containment only`
}
