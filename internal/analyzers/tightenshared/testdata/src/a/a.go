package a

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"fileperm"
)

// logSink is battery/log FileSink (L20): the log path's parent.
func logSink(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil { // want `tightenshared: os.Chmod tightens the caller-named directory parent`
		return err
	}
	return fileperm.Restrict(parent, true) // one report per directory
}

// exportTo is ExportData (S43): the export directory itself.
func exportTo(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fileperm.Restrict(dir, true) // want `tightenshared: fileperm.Restrict tightens the caller-named directory dir`
}

// snapshotDir is the semantic Snapshot (B62): Dir of the param inline.
func snapshotDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileperm.Restrict(filepath.Dir(path), true) // want `tightenshared: fileperm.Restrict tightens`
}

// createdOnly is the fix: Stat first, tighten only what this call made.
func createdOnly(dir string) error {
	_, err := os.Stat(dir)
	created := errors.Is(err, fs.ErrNotExist)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if created {
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// statAfter does not count: the check must come before MkdirAll.
func statAfter(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(dir); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700) // want `tightenshared: os.Chmod tightens`
}

type runtime struct{ projectDir, id string }

// ownDir is the isolation runtime: the package names the directory.
func (r *runtime) ownDir() error {
	dir := filepath.Join(r.projectDir, ".gofastr", "isolation", r.id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fileperm.Restrict(dir, true)
}

// joinedUnder is the desktop default data dir: a param joined under a
// base the package picks is the package's own directory.
func joinedUnder(id string) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fileperm.Restrict(dir, true)
}

// restoreMode is a zip extract: the mode is the archive's own.
func restoreMode(target string, mode os.FileMode) error {
	if err := os.MkdirAll(target, mode); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}

// groupMode leaves group bits: not an owner-only tightening.
func groupMode(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.Chmod(dir, 0o750)
}

// fileRestrict restricts a file, not a directory.
func fileRestrict(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return fileperm.Restrict(path, false)
}

// noMkdir tightens a directory the caller just made: no MkdirAll here.
func noMkdir(dir string) error {
	return fileperm.Restrict(dir, true)
}

// otherDir tightens a different directory than the one it made.
func otherDir(dir, other string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(other, 0o700)
}

// treeWalk is the storage chain: out of reach, documented.
func treeWalk(dir, root string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fileperm.RestrictDirectoryTree(dir, root)
}
