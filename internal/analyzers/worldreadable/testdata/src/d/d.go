// Package d pins the restrict arm (2026-10-28, B60): an owner-only
// OpenFile whose author relies on fileperm.Restrict for a file that may
// already exist.
package d

import (
	"os"
	"path/filepath"

	"fileperm"
)

// openWAL is battery/semantic openWAL (B60).
func openWAL(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600) // want `os\.OpenFile mode 0o600 applies only at create, and fileperm\.Restrict`
	if err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// viaLocal resolves the path through a local on both sides.
func viaLocal(dir string) error {
	p := filepath.Join(dir, "wal.log")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, 0o600) // want `os\.OpenFile mode 0o600 applies only at create`
	if err != nil {
		return err
	}
	defer f.Close()
	return fileperm.Restrict(filepath.Join(dir, "wal.log"), false)
}

// handleRestricted is the fix: the handle carries the mode.
func handleRestricted(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if err := fileperm.RestrictOpenFile(f); err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// handleChmod is the fix spelled with the handle's own Chmod.
func handleChmod(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

type perms struct{}

func (perms) Chmod(mode os.FileMode) error { return nil }

// otherChmod calls a Chmod that is not the file handle's.
func otherChmod(path string, p perms) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // want `os\.OpenFile mode 0o600 applies only at create`
	if err != nil {
		return nil, err
	}
	if err := p.Chmod(0o600); err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// pathChmod tightens by path after the open: the mode is fixed.
func pathChmod(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// exclusive cannot open an existing file.
func exclusive(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// lockFile never claims owner-only for an existing file: no Restrict.
func lockFile(dir string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDWR|os.O_CREATE, 0o600)
}

// noCreate opens an existing file only.
func noCreate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(path, false)
}

// otherPath restricts a different file than the one it opened.
func otherPath(path, other string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return f, fileperm.Restrict(other, false)
}
