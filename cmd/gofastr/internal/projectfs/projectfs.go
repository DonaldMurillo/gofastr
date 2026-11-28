// Package projectfs reads and writes a project's files without following a symlink out of it.
package projectfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const maxLinkHops = 40

// OpenRoot creates dir if needed and opens it as an os.Root.
func OpenRoot(dir string) (*os.Root, error) {
	//gofastr:allow(worldreadable) dir is the project checkout, which holds committed source and docs; 0755 is the mode it would get from git clone or mkdir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// Resolve returns the slash path under root that name really names, with
// every symlink on the way replaced by its target; it errors on a link
// that leaves root.
func Resolve(root *os.Root, name string) (string, error) {
	pending := strings.Split(filepath.ToSlash(name), "/")
	var done []string
	missing := false
	hops := 0
	var lastLink, lastTarget string
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			if len(done) == 0 {
				if lastLink != "" {
					return "", outErr(lastLink, lastTarget)
				}
				return "", fmt.Errorf("%q leaves the project", name)
			}
			done = done[:len(done)-1]
			continue
		}
		cur := path.Join(path.Join(done...), c)
		if missing {
			done = append(done, c)
			continue
		}
		fi, err := root.Lstat(filepath.FromSlash(cur))
		if errors.Is(err, fs.ErrNotExist) {
			missing = true
			done = append(done, c)
			continue
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			done = append(done, c)
			continue
		}
		hops++
		if hops > maxLinkHops {
			return "", fmt.Errorf("%q: too many levels of symbolic links", name)
		}
		target, err := root.Readlink(filepath.FromSlash(cur))
		if err != nil {
			return "", err
		}
		slashTarget := filepath.ToSlash(target)
		if filepath.IsAbs(target) || path.IsAbs(slashTarget) || filepath.VolumeName(target) != "" {
			return "", outErr(cur, target)
		}
		lastLink, lastTarget = cur, target
		pending = append(strings.Split(slashTarget, "/"), pending...)
	}
	if len(done) == 0 {
		return ".", nil
	}
	return path.Join(done...), nil
}

func outErr(link, target string) error {
	return fmt.Errorf("%q is a symlink out of the project (to %q); move it, then re-run", link, target)
}

// Check errors when a symlink on the way to name, or name itself, leads out of dir.
func Check(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	_, err = Resolve(root, name)
	return err
}

// Read reads name under root, following a link only while it stays in root.
func Read(root *os.Root, name string) ([]byte, error) {
	resolved, err := Resolve(root, name)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(filepath.FromSlash(resolved))
}

// Lstat reports on name under root without following a link at name itself.
func Lstat(root *os.Root, name string) (fs.FileInfo, error) {
	dir, base := path.Split(filepath.ToSlash(name))
	p, err := resolveParent(root, dir, base)
	if err != nil {
		return nil, err
	}
	return root.Lstat(filepath.FromSlash(p))
}

func resolveParent(root *os.Root, dir, base string) (string, error) {
	resolved, err := Resolve(root, dir)
	if err != nil {
		return "", err
	}
	return path.Join(resolved, base), nil
}

// Replace atomically replaces name under root with data, creating it with perm.
func Replace(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	return replace(root, name, data, func(existing fs.FileInfo) (fs.FileMode, func(*os.File) error) {
		if existing == nil {
			return perm, nil
		}
		return 0o600, atomicfile.Mode(existing.Mode().Perm())
	})
}

// ReplaceFixedMode replaces name under root with data at mode, refusing a link at name.
func ReplaceFixedMode(root *os.Root, name string, data []byte, mode fs.FileMode) error {
	return replaceAt(root, name, data, true, func(fs.FileInfo) (fs.FileMode, func(*os.File) error) {
		return 0o600, atomicfile.Mode(mode)
	})
}

// ReplaceOwnerOnly is Replace into an owner-only file, refusing what RegularLeaf refuses.
func ReplaceOwnerOnly(root *os.Root, name string, data []byte) error {
	resolved, err := RegularLeaf(root, name)
	if err != nil {
		return err
	}
	return replaceAt(root, resolved, data, false, func(fs.FileInfo) (fs.FileMode, func(*os.File) error) {
		return 0o600, fileperm.RestrictOpenFile
	})
}

// RegularLeaf resolves name's directory and errors unless name is missing or a regular file.
func RegularLeaf(root *os.Root, name string) (string, error) {
	dir, base := path.Split(strings.TrimRight(filepath.ToSlash(name), "/"))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("%q names no file", name)
	}
	p, err := resolveParent(root, dir, base)
	if err != nil {
		return "", err
	}
	fi, err := root.Lstat(filepath.FromSlash(p))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return p, nil
	case err != nil:
		return "", err
	case fi.Mode()&fs.ModeSymlink != 0:
		return "", fmt.Errorf("%q is a symlink; gofastr writes a secret into it, so it must be a regular file: put the file there instead of the link, then re-run", name)
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("%q is not a regular file; gofastr writes a secret into it: move it aside, then re-run", name)
	}
	return p, nil
}

// CheckRegularLeaf is RegularLeaf on an os.Root on dir.
func CheckRegularLeaf(dir, name string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	_, err = RegularLeaf(root, name)
	return err
}

func replace(root *os.Root, name string, data []byte, modeFor func(existing fs.FileInfo) (fs.FileMode, func(*os.File) error)) error {
	name, err := Resolve(root, name)
	if err != nil {
		return err
	}
	return replaceAt(root, name, data, false, modeFor)
}

func replaceAt(root *os.Root, name string, data []byte, refuseLeafLink bool, modeFor func(existing fs.FileInfo) (fs.FileMode, func(*os.File) error)) error {
	var existing fs.FileInfo
	if fi, serr := root.Lstat(filepath.FromSlash(name)); serr == nil {
		isLink := fi.Mode()&fs.ModeSymlink != 0
		if isLink && refuseLeafLink {
			return fmt.Errorf("%q is a symlink; the planned file must be a regular file", name)
		}
		if !isLink && fi.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		if fi.Mode().IsRegular() {
			existing = fi
		}
	}
	createMode, prepare := modeFor(existing)
	return atomicfile.WriteFileAt(root, name, data, createMode, prepare)
}

// ReadFile is Read on an os.Root on dir.
func ReadFile(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return Read(root, name)
}

// WriteFile is Replace on an os.Root on dir.
func WriteFile(dir, name string, data []byte, perm fs.FileMode) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return Replace(root, name, data, perm)
}

// WriteOwnerOnly is ReplaceOwnerOnly on an os.Root on dir.
func WriteOwnerOnly(dir, name string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return ReplaceOwnerOnly(root, name, data)
}

// MkdirAll creates name and its parents through an os.Root on dir.
func MkdirAll(dir, name string, perm fs.FileMode) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(filepath.FromSlash(name), perm)
}

// LstatFile is Lstat on an os.Root on dir.
func LstatFile(dir, name string) (fs.FileInfo, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return Lstat(root, name)
}

// UserPath is Split, creating the returned directory when it is not ".".
func UserPath(p string) (dir, name string, err error) {
	dir, name = Split(p)
	if dir != "." {
		//gofastr:allow(worldreadable) a directory the user named for a source file of their project; 0755 is the mode mkdir gives it
		err = os.MkdirAll(dir, 0o755)
	}
	return dir, name, err
}

// Split splits a path the user named into the directory to confine to and the name under it.
func Split(p string) (dir, name string) {
	if filepath.IsLocal(p) {
		return ".", filepath.ToSlash(filepath.Clean(p))
	}
	if filepath.IsAbs(p) {
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, p); err == nil && filepath.IsLocal(rel) {
				return wd, filepath.ToSlash(rel)
			}
			if rel, ok := underDir(wd, p); ok {
				return wd, rel
			}
		}
	}
	return filepath.Dir(p), filepath.Base(p)
}

func underDir(dir, p string) (string, bool) {
	want, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	p = filepath.Clean(p)
	for a := filepath.Dir(p); ; a = filepath.Dir(a) {
		if under, ok := ancestorUnder(a, want, realDir); ok {
			rest, err := filepath.Rel(a, p)
			if err != nil || !filepath.IsLocal(rest) {
				return "", false
			}
			return filepath.ToSlash(filepath.Join(under, rest)), true
		}
		if parent := filepath.Dir(a); parent == a {
			return "", false
		}
	}
}

func ancestorUnder(a string, want fs.FileInfo, realDir string) (string, bool) {
	fi, err := os.Stat(a)
	if err != nil || !fi.IsDir() {
		return "", false
	}
	if os.SameFile(fi, want) {
		return ".", true
	}
	real, err := filepath.EvalSymlinks(a)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realDir, real)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}
