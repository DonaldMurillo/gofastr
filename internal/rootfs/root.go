// Package rootfs provides kernel-contained reads without idle directory handles.
package rootfs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

// Root remembers a directory's identity and reopens it for each operation.
type Root struct {
	path     string
	identity os.FileInfo
}

func Open(path string) (*Root, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	identity, err := directoryInfo(r)
	if err != nil {
		return nil, err
	}
	return &Root{path: abs, identity: identity}, nil
}

func directoryInfo(r *os.Root) (os.FileInfo, error) {
	f, err := r.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

func (r *Root) reopen() (*os.Root, error) {
	root, err := os.OpenRoot(r.path)
	if err != nil {
		return nil, err
	}
	info, err := directoryInfo(root)
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(r.identity, info) {
		root.Close()
		return nil, fs.ErrNotExist
	}
	return root, nil
}

func (r *Root) Open(name string) (*os.File, error) {
	root, err := r.reopen()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(name)
}

func (r *Root) Stat(name string) (os.FileInfo, error) {
	root, err := r.reopen()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(name)
}

func (r *Root) ReadFile(name string) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (r *Root) FS() fs.FS { return fileSystem{r} }

type fileSystem struct{ root *Root }

func (f fileSystem) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return f.root.Open(filepath.FromSlash(name))
}

// dirFSType is the dynamic type os.DirFS returns (the unexported
// os.dirFS, a string type holding the directory), captured by
// constructing one so reflect can recognize the shape without naming
// the unexported type.
var dirFSType = reflect.TypeOf(os.DirFS(""))

// ConfineDirFS returns fsys unchanged unless it is an os.DirFS value, in
// which case it returns the kernel-contained [Root.FS] view over the
// same directory: a symlink inside the directory pointing outside it is
// refused at open instead of followed. When the directory cannot be
// opened as a root it returns an FS whose every open fails with the
// reason, since reads from a directory that could not be verified are
// the worse failure. embed.FS and every other fs.FS pass through.
func ConfineDirFS(fsys fs.FS) fs.FS {
	if fsys == nil || reflect.TypeOf(fsys) != dirFSType {
		return fsys
	}
	dir := reflect.ValueOf(fsys).String()
	root, err := Open(dir)
	if err != nil {
		return failClosedFS{err: fmt.Errorf("directory FS rooted at an unopenable directory: %w", err)}
	}
	return root.FS()
}

// failClosedFS is ConfineDirFS's answer for an os.DirFS whose directory
// cannot be opened as a root.
type failClosedFS struct{ err error }

func (m failClosedFS) Open(string) (fs.File, error) { return nil, m.err }
