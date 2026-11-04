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

// ignoredStat discards the result (AZ12-03, 2026-11): the call was
// counted as the existence check on its own, result consulted or not.
func ignoredStat(dir string) error {
	_, _ = os.Stat(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700) // want `tightenshared: os.Chmod tightens the caller-named directory dir`
}

// bareStat drops the results entirely.
func bareStat(dir string) error {
	os.Stat(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700) // want `tightenshared: os.Chmod tightens the caller-named directory dir`
}

// statInIfInit: the existence check inside the if's Init, its error
// compared in the Cond. The fix posture. Quiet.
func statInIfInit(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil // existed: leave its mode alone
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// mkdirTolerated is the desktop profile walk spelling (AZ12-04,
// 2026-11): a single-level Mkdir whose EEXIST is a concurrent win,
// then the same tightening of a caller-named directory. The arm was
// MkdirAll-only, so this stayed silent.
func mkdirTolerated(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	return os.Chmod(dir, 0o700) // want `tightenshared: os.Chmod tightens the caller-named directory dir`
}

// mkdirErrsIs: the errors.Is(err, fs.ErrExist) spelling arms too.
func mkdirErrsIs(dir string) error {
	err := os.Mkdir(dir, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return fileperm.Restrict(dir, true) // want `tightenshared: fileperm.Restrict tightens the caller-named directory dir`
}

// mkdirStrict refuses the EEXIST race: an existing directory never
// reaches the tighten. Quiet.
func mkdirStrict(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// mkdirToleratedThenStat: a consulted Stat still exempts, Mkdir arm or
// not. Quiet.
func mkdirToleratedThenStat(dir string) error {
	_, err := os.Stat(dir)
	created := errors.Is(err, fs.ErrNotExist)
	if mkErr := os.Mkdir(dir, 0o700); mkErr != nil && !os.IsExist(mkErr) {
		return mkErr
	}
	if !created {
		return nil
	}
	return os.Chmod(dir, 0o700)
}
