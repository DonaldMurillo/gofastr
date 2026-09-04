// Package e holds the rootwrite spellings the 2026-09-04 red-probe
// round proved were blind spots (battery/storage local.go, probe
// TestLocalStorageSymlinkEscapeRefused, reduced here under different
// identifiers): a same-package helper that RETURNS the root-joined
// path, rename/link/symlink destinations, remove, and MkdirAll of the
// destination's Dir — each with its fixed spelling next to it.
package e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// depot is the storage-receiver shape: the Join lives in locate, and
// the callers only ever see its result.
type depot struct {
	vaultDir string
}

func rejectBadSlug(slug string) error {
	if filepath.IsAbs(slug) {
		return errors.New("absolute slug")
	}
	return nil
}

// locate returns the absolute path for a slug: Join plus a format
// check. Lexical containment only — the pre-fix posture.
func (d *depot) locate(slug string) (string, error) {
	if err := rejectBadSlug(slug); err != nil {
		return "", err
	}
	return filepath.Join(d.vaultDir, slug), nil
}

// put is the Save shape: MkdirAll on the Dir of the helper-returned
// path, then an atomic rename into place. Both are writes under the
// root, and neither is a sink the Join used to reach directly.
func (d *depot) put(slug string, payload []byte) error {
	dst, err := d.locate(slug)
	if err != nil {
		return err
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	tmp, err := os.CreateTemp(dir, ".staging-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	target, err := d.locate(slug)
	if err != nil {
		return err
	}

	err = os.Remove(target) // want `write under a root with lexical containment only`
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("purge %q: %w", slug, err)
	}
	return nil
}

// materialize covers the two remaining destination sinks: hard and
// symbolic links placed at the helper-returned path.
func materialize(d *depot, slug, origin string) error {
	dst, err := d.locate(slug)
	if err != nil {
		return err
	}
	if err := os.Link(origin, dst); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	alias, err := d.locate(slug + ".alias")
	if err != nil {
		return err
	}
	return os.Symlink(origin, alias) // want `write under a root with lexical containment only`
}

// locateResolved is the fix posture at the helper: its body resolves
// the directory chain, so every caller of it stays quiet.
func (d *depot) locateResolved(slug string) (string, error) {
	if err := rejectBadSlug(slug); err != nil {
		return "", err
	}
	p := filepath.Join(d.vaultDir, slug)
	real, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	if real != d.vaultDir {
		return "", errors.New("escaped")
	}
	return p, nil
}

// putFixed renames into a resolved destination: the write's own chain
// was checked, not just the slug format.
func (d *depot) putFixed(slug string, payload []byte) error {
	dst, err := d.locateResolved(slug)
	if err != nil {
		return err
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".staging-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

// purgeFixed unlinks a leaf that was Lstat-checked to not be a
// symlink, under a resolved directory chain.
func (d *depot) purgeFixed(slug string) error {
	target, err := d.locateResolved(slug)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if fi != nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return os.Remove(target)
}

// renameIntoTmp moves a caller file into a throwaway directory: a
// MkdirTemp root is silent, source and destination alike.
func renameIntoTmp(name string, data []byte) error {
	dir, err := os.MkdirTemp("", "staging")
	if err != nil {
		return err
	}
	return os.Rename(filepath.Join("/tmp", name), filepath.Join(dir, name))
}

// renameOutOfRoot renames FROM a root-derived path to a literal
// destination: only the destination is a write, and a literal names
// nothing caller-controlled.
func renameOutOfRoot(root, name string) error {
	return os.Rename(filepath.Join(root, name), "/var/spool/inbox")
}

// mkdirAllOfLiteral creates a fixed subdirectory of a root: no
// caller-controlled component, nothing to escape with.
func mkdirAllOfLiteral(root string) error {
	return os.MkdirAll(filepath.Dir(filepath.Join(root, "manifest.json")), 0o755)
}

// removeScratch removes under a throwaway root.
func removeScratch(name string) error {
	dir, err := os.MkdirTemp("", "scratch")
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, name))
}

// locateHelperLiteral: a helper-returned path with nothing
// caller-controlled appended — the literal-only hop stays quiet.
func (d *depot) locateHelperLiteral() (string, error) {
	return d.locate("manifest.json")
}

func writeManifest(d *depot) error {
	p, err := d.locateHelperLiteral()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte("{}"), 0o644)
}
