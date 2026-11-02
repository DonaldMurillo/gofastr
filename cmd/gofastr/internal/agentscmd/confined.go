package agentscmd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// The onboarding files this package writes (AGENTS.md, the .agents/
// detail files, the gofastr-host skill) land in a project the user may
// have cloned from anyone. Every read and write goes through an os.Root
// opened on the project directory, so a symlink in it, whether a
// directory on the way or the file itself, cannot carry a write outside
// the project: os.Root refuses a link that leaves it. A link that stays
// in the project is followed, as a write through it would be.

// openProjectRoot opens dir, creating it first, as the root every
// onboarding write is confined to.
func openProjectRoot(dir string) (*os.Root, error) {
	//gofastr:allow(worldreadable) dir is the project checkout, which holds committed source and docs; 0755 is the mode it would get from git clone or mkdir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// maxLinkHops bounds how many symlinks resolveLeaf follows, as the
// kernel's own ELOOP limit does.
const maxLinkHops = 40

// resolveLeaf follows a symlink at name (slash-separated, relative to
// root) to the path it names inside root, so a replace lands on the
// link's target and the link stays a link. A link that names an absolute
// path or climbs out of root is refused. A missing name, or one that is
// not a link, is returned as it is.
func resolveLeaf(root *os.Root, name string) (string, error) {
	for range maxLinkHops {
		fi, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			return name, nil
		}
		target, err := root.Readlink(name)
		if err != nil {
			return "", err
		}
		next := path.Join(path.Dir(name), filepath.ToSlash(target))
		if filepath.IsAbs(target) || path.IsAbs(target) || !filepath.IsLocal(filepath.FromSlash(next)) {
			return "", fmt.Errorf("%s is a symlink out of the project (to %q); move it, then re-run", name, target)
		}
		name = next
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", name)
}

// readInRoot reads name under root, following a link only while it stays
// in root.
func readInRoot(root *os.Root, name string) ([]byte, error) {
	resolved, err := resolveLeaf(root, name)
	if err != nil {
		return nil, err
	}
	return root.ReadFile(resolved)
}

// replaceInRoot replaces name under root with data through a temporary
// file beside it, synced and renamed over it, so a crash mid-write leaves
// the old file or the new one, never a truncated one. A link at name is
// followed while it stays in root (resolveLeaf); the rename then replaces
// the target, never the link, and never anything outside root. An
// existing file keeps its mode; a new one gets perm. A file without its
// owner write bit is refused, as opening it for writing would be.
func replaceInRoot(root *os.Root, name string, data []byte, perm fs.FileMode) (err error) {
	name, err = resolveLeaf(root, name)
	if err != nil {
		return err
	}
	mode := perm
	if fi, serr := root.Stat(name); serr == nil {
		if fi.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		mode = fi.Mode().Perm()
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := path.Join(path.Dir(name), "."+path.Base(name)+".tmp-"+hex.EncodeToString(nonce[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(tmp)
		}
	}()
	if err = f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
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
	return root.Rename(tmp, name)
}
