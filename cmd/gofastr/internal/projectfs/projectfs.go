// Package projectfs reads and writes files inside a user's project
// without following a symlink out of it.
//
// The CLI writes into checkouts the user may have cloned from anyone. A
// path joined onto the project directory and handed to os.WriteFile,
// os.ReadFile or atomicfile.Rewrite follows every symlink on the way, so
// a repo carrying .gitignore -> ~/.bashrc, or static -> /elsewhere, had
// `gofastr init .` append its ignore rules to the user's shell profile
// and create files in a directory the project does not own. Every
// operation here goes through an os.Root on the project directory: a
// link that stays in the project is followed, the way a write through it
// would be; one that leaves (an absolute target, or a relative one that
// climbs past the root) is refused, dangling or not.
package projectfs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// maxLinkHops bounds how many symlinks Resolve follows, as the kernel's
// own ELOOP limit does.
const maxLinkHops = 40

// OpenRoot opens dir, creating it first, as the root a command's writes
// are confined to.
func OpenRoot(dir string) (*os.Root, error) {
	//gofastr:allow(worldreadable) dir is the project checkout, which holds committed source and docs; 0755 is the mode it would get from git clone or mkdir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// Resolve returns the path name (slash-separated, relative to root)
// really names inside root, with every symlink on the way replaced by
// its target, so a replace lands on the file a link points at and the
// link stays a link. A relative target is read from the directory the
// link really sits in, which is the resolved parent, not the lexical
// one: with docs -> real/sub, docs/x -> ../y is real/y. A link to an
// absolute path, or one whose target climbs out of root, is refused. A
// missing component ends the walk; the rest of name is joined as given.
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
				return "", fmt.Errorf("%s leaves the project", name)
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
			return "", fmt.Errorf("%s: too many levels of symbolic links", name)
		}
		target, err := root.Readlink(filepath.FromSlash(cur))
		if err != nil {
			return "", err
		}
		slashTarget := filepath.ToSlash(target)
		if filepath.IsAbs(target) || path.IsAbs(slashTarget) || filepath.VolumeName(target) != "" {
			return "", outErr(cur, target)
		}
		// The target is read from the link's own (already resolved)
		// directory: done is left as it is and the target's components go
		// first in line. A ".." in them that pops past root is an escape,
		// refused above with this link named.
		lastLink, lastTarget = cur, target
		pending = append(strings.Split(slashTarget, "/"), pending...)
	}
	if len(done) == 0 {
		return ".", nil
	}
	return path.Join(done...), nil
}

func outErr(link, target string) error {
	return fmt.Errorf("%s is a symlink out of the project (to %q); move it, then re-run", link, target)
}

// Read reads name under root, following a link only while it stays in
// root.
func Read(root *os.Root, name string) ([]byte, error) {
	resolved, err := Resolve(root, name)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(filepath.FromSlash(resolved))
}

// Lstat reports on name under root without following a link at name
// itself; a link on the way to it is followed only while it stays in
// root.
func Lstat(root *os.Root, name string) (fs.FileInfo, error) {
	dir, base := path.Split(filepath.ToSlash(name))
	resolved := "."
	if dir != "" {
		var err error
		if resolved, err = Resolve(root, dir); err != nil {
			return nil, err
		}
	}
	return root.Lstat(filepath.FromSlash(path.Join(resolved, base)))
}

// Replace replaces name under root with data through a temporary file
// beside it, synced and renamed over it, so a crash mid-write leaves the
// old file or the new one, never a truncated one. A link at name is
// followed while it stays in root (Resolve); the rename then replaces
// the target, never the link, and never anything outside root. An
// existing file keeps its mode. A new one is created with perm, and the
// process umask applies, as it does to os.WriteFile. A file without its
// owner write bit is refused, as opening it for writing would be.
func Replace(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	return replace(root, name, data, func(existing fs.FileInfo) (fs.FileMode, func(*os.File) error) {
		if existing == nil {
			return perm, nil
		}
		mode := existing.Mode().Perm()
		return 0o600, func(f *os.File) error { return f.Chmod(mode) }
	})
}

// ReplaceOwnerOnly is Replace for a file that holds secrets (a .env, the
// settings file): the temporary file is owner-only (0600, or an
// owner-only ACL on Windows, fileperm.RestrictOpenFile) before a byte
// lands, whatever mode a file already there had.
func ReplaceOwnerOnly(root *os.Root, name string, data []byte) error {
	return replace(root, name, data, func(fs.FileInfo) (fs.FileMode, func(*os.File) error) {
		return 0o600, fileperm.RestrictOpenFile
	})
}

func replace(root *os.Root, name string, data []byte, modeFor func(existing fs.FileInfo) (fs.FileMode, func(*os.File) error)) (err error) {
	name, err = Resolve(root, name)
	if err != nil {
		return err
	}
	var existing fs.FileInfo
	if fi, serr := root.Lstat(filepath.FromSlash(name)); serr == nil {
		if fi.Mode()&fs.ModeSymlink == 0 && fi.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		if fi.Mode().IsRegular() {
			existing = fi
		}
	}
	createMode, prepare := modeFor(existing)
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.FromSlash(path.Join(path.Dir(name), "."+path.Base(name)+".tmp-"+hex.EncodeToString(nonce[:])))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, createMode)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(tmp)
		}
	}()
	if prepare != nil {
		if err = prepare(f); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, filepath.FromSlash(name))
}

// ReadFile reads name (slash-separated, relative to dir) through an
// os.Root on dir: a symlink is followed only while it stays in dir, so a
// project file linked out of the project reads as an error, never as the
// file it points at.
func ReadFile(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return Read(root, name)
}

// WriteFile replaces name (slash-separated, relative to dir) with data
// through Replace on an os.Root on dir: a link inside dir is written
// through, one out of dir is refused, an existing file keeps its mode
// and a new one gets perm less the umask.
func WriteFile(dir, name string, data []byte, perm fs.FileMode) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return Replace(root, name, data, perm)
}

// WriteOwnerOnly replaces name (relative to dir) with data through
// ReplaceOwnerOnly on an os.Root on dir.
func WriteOwnerOnly(dir, name string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return ReplaceOwnerOnly(root, name, data)
}

// MkdirAll creates name (relative to dir) and its parents through an
// os.Root on dir: an existing link on the way is followed only while it
// stays in dir.
func MkdirAll(dir, name string, perm fs.FileMode) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(filepath.FromSlash(name), perm)
}

// LstatFile is Lstat on an os.Root on dir: a link at name is reported as
// a link (so a dangling one counts as a file that is already there),
// and a link on the way to it is followed only while it stays in dir.
func LstatFile(dir, name string) (fs.FileInfo, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return Lstat(root, name)
}

// UserPath splits a path the user named and makes sure the directory it
// is confined to exists: a non-local path names its own directory on
// purpose (Split), and that directory is created as given; a local one
// is confined to the working directory, whose subdirectories the caller
// creates through MkdirAll.
func UserPath(p string) (dir, name string, err error) {
	dir, name = Split(p)
	if dir != "." {
		//gofastr:allow(worldreadable) a directory the user named for a source file of their project; 0755 is the mode mkdir gives it
		err = os.MkdirAll(dir, 0o755)
	}
	return dir, name, err
}

// Split splits a path the user named into the directory to confine to
// and the name under it. A local path (theme/theme.go) is confined to
// the working directory, so a symlinked theme/ in the checkout cannot
// carry the write out. A path that is absolute or climbs (../x, /abs/x)
// names its own directory on purpose; only its last element is confined.
func Split(p string) (dir, name string) {
	if filepath.IsLocal(p) {
		return ".", filepath.ToSlash(filepath.Clean(p))
	}
	return filepath.Dir(p), filepath.Base(p)
}
