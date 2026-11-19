package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
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
	_, err = writeFile(path, "."+filepath.Base(path)+".tmp-*", data, prepare, true)
	return err
}

// WriteFileAt replaces name (slash-separated, already resolved inside
// root) with data through a sibling temporary file created, synced and
// renamed through root: the rooted twin of WriteFile, with confinement
// and leaf checks left to the caller that resolved the name. createMode
// is the mode a new file is created with; prepare runs on the temporary
// file before a byte lands. On Windows the rename retries transient
// failures the way Replace does.
func WriteFileAt(root *os.Root, name string, data []byte, createMode os.FileMode, prepare func(*os.File) error) (err error) {
	var nonce [8]byte
	if _, nerr := rand.Read(nonce[:]); nerr != nil {
		return nerr
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
	return ReplaceAt(root, tmp, filepath.FromSlash(name))
}

// WriteStage identifies the operation that failed during atomic replacement.
type WriteStage uint8

const (
	WriteCreateTemp WriteStage = iota + 1
	WritePrepareTemp
	WriteData
	writeSync
	WriteCloseTemp
	WriteReplace
)

// WriteFileNoFollow replaces the final directory entry, including a symlink,
// with a prepared sibling temporary file without syncing. It returns the stage on failure.
func WriteFileNoFollow(path string, data []byte, prepare func(*os.File) error) (WriteStage, error) {
	return writeFile(path, ".gofastr-tmp-*", data, prepare, false)
}

func writeFile(path, pattern string, data []byte, prepare func(*os.File) error, sync bool) (stage WriteStage, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return WriteCreateTemp, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if prepare != nil {
		if err = prepare(tmp); err != nil {
			_ = tmp.Close()
			return WritePrepareTemp, err
		}
	}
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return WriteData, err
	}
	if sync {
		if err = tmp.Sync(); err != nil {
			_ = tmp.Close()
			return writeSync, err
		}
	}
	if err = tmp.Close(); err != nil {
		return WriteCloseTemp, err
	}
	if err = Replace(tmp.Name(), path); err != nil {
		return WriteReplace, err
	}
	return 0, nil
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
