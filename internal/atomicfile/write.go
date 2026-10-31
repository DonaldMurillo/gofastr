package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile replaces path with data so that a crash or a full disk
// leaves the old file or the new one, never a truncated one: data goes
// to a temporary file beside path, which is synced, closed and renamed
// over it. prepare runs on the temporary file before a byte lands; set
// its mode there (Mode, or fileperm.RestrictOpenFile for an owner-only
// file). A symlink at path is followed and its target replaced, as a
// write through it would be (a CLAUDE.md linked to AGENTS.md stays a
// link). A file without its owner write bit is refused, as opening it
// for writing would be; a rename would replace it regardless. The
// replaced file is a new inode: a hard link to the old one keeps the
// old content.
func WriteFile(path string, data []byte, prepare func(*os.File) error) (err error) {
	if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if path, err = filepath.EvalSymlinks(path); err != nil {
			return err
		}
	}
	if fi, serr := os.Stat(path); serr == nil && fi.Mode().Perm()&0o200 == 0 {
		return &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if prepare != nil {
		if err = prepare(tmp); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return Replace(tmp.Name(), path)
}

// Mode is a WriteFile prepare step that sets the file's permission bits
// (the temporary file starts at 0600).
func Mode(perm os.FileMode) func(*os.File) error {
	return func(f *os.File) error { return f.Chmod(perm) }
}

// Rewrite writes data to path as os.WriteFile(path, data, perm) does,
// except that a file already there is replaced through WriteFile and
// keeps its mode. A new file has nothing to lose, so it is created
// directly and the umask applies to perm.
func Rewrite(path string, data []byte, perm os.FileMode) error {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(path, data, perm)
	}
	if err != nil {
		return err
	}
	return WriteFile(path, data, Mode(fi.Mode().Perm()))
}
