// Package f holds the rootwrite fixtures for the 2026-09-04 round-3
// posture change. The reviewer's mutation of core/upload Save — both
// filepath.EvalSymlinks calls deleted — stayed silent behind
// sanitizeKey's result replacing the joined component; reduced here
// under different identifiers it is the must-fire oracle, with the
// unmutated fix, the os.Root spellings, and the leaf postures beside
// it.
package f

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// cellar is the LocalStorage shape: caller-named objects under a rooty
// field, sanitized before every operation.
type cellar struct {
	vaultDir string
}

func filterRef(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("empty ref")
	}
	cleaned := filepath.Clean(ref)
	if cleaned == "." {
		return "", errors.New("empty after cleaning")
	}
	return cleaned, nil
}

// decantLexical is the MUTATED Save: the sanitizer's result feeds the
// Join, the containment check is lexical (Abs + prefix), and both
// EvalSymlinks calls the real Save carries are gone. The rule was
// silent here before the posture change — exactly the miss the
// mutation proved — and every write sink fires now: a name sanitizer
// cannot see a symlinked directory.
func (c *cellar) decantLexical(ref string, payload []byte) error {
	safe, err := filterRef(ref)
	if err != nil {
		return err
	}
	full := filepath.Join(c.vaultDir, safe)
	absVault, err := filepath.Abs(c.vaultDir)
	if err != nil {
		return err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absFull, absVault+string(os.PathSeparator)) && absFull != absVault {
		return errors.New("escapes base")
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o700); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // want `write under a root with lexical containment only`
	if err != nil {
		return err
	}
	if _, err := f.Write(payload); err != nil {
		f.Close()
		_ = os.Remove(full) // want `write under a root with lexical containment only`
		return err
	}
	return f.Close()
}

// decantResolved is the unmutated core/upload Save: the same sanitized
// join with both EvalSymlinks calls the mutation deleted — the storage
// root and the destination's parent — back in place. Resolution on the
// write's own chain, sanitizer and all.
func (c *cellar) decantResolved(ref string, payload []byte) error {
	safe, err := filterRef(ref)
	if err != nil {
		return err
	}
	full := filepath.Join(c.vaultDir, safe)
	absVault, err := filepath.Abs(c.vaultDir)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	realVault, err := filepath.EvalSymlinks(absVault)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(realDir, realVault+string(os.PathSeparator)) && realDir != realVault {
		return errors.New("escapes through a symlink")
	}
	if err := os.WriteFile(full, payload, 0o600); err != nil {
		_ = os.Remove(full) // resolved chain: the cleanup spell is quiet too
		return err
	}
	return nil
}

// decantViaRoot is the strongest fix posture on Go 1.27: os.OpenRoot
// confines every operation to the root in the kernel — a symlink under
// it cannot lead the write out, and there is no TOCTOU window between
// check and use. Root methods are not os.* sinks, so the rule is quiet
// by construction; this fixture keeps it that way.
func (c *cellar) decantViaRoot(ref string, payload []byte) error {
	rt, err := os.OpenRoot(c.vaultDir)
	if err != nil {
		return err
	}
	defer rt.Close()
	if err := rt.MkdirAll(filepath.Dir(ref), 0o700); err != nil {
		return err
	}
	return rt.WriteFile(ref, payload, 0o600)
}

// stampViaRoot covers the Root create spellings — Create and a write
// OpenFile — on names joined under the root.
func stampViaRoot(scratchRoot, sub, name string) (*os.File, error) {
	rt, err := os.OpenRoot(scratchRoot)
	if err != nil {
		return nil, err
	}
	defer rt.Close()
	f, err := rt.Create(filepath.Join(sub, name))
	if err != nil {
		return nil, err
	}
	g, err := rt.OpenFile(filepath.Join(sub, name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return g, nil
}

// swapViaRoot is the atomic-rename idiom under a kernel-contained
// root: the destination a storage Save renames into place.
func swapViaRoot(scratchRoot, staging, final string) error {
	rt, err := os.OpenRoot(scratchRoot)
	if err != nil {
		return err
	}
	defer rt.Close()
	return rt.Rename(staging, final)
}

// purgeViaRoot unlinks through the kernel-contained root.
func purgeViaRoot(scratchRoot, sub, name string) error {
	rt, err := os.OpenRoot(scratchRoot)
	if err != nil {
		return err
	}
	defer rt.Close()
	return rt.Remove(filepath.Join(sub, name))
}

// stashLexical is the contrast for the Root fixtures above: the
// identical joined name through the package-level sink is lexical
// containment only.
func stashLexical(scratchRoot, sub, name string, payload []byte) error {
	return os.WriteFile(filepath.Join(scratchRoot, sub, name), payload, 0o600) // want `write under a root with lexical containment only`
}

// stampNoFollow refuses the symlinked leaf in the open flags
// themselves: a documented partial fix — the directory components
// above the leaf stay the writer's problem.
func stampNoFollow(spoolDir, id string) (*os.File, error) {
	return os.OpenFile(filepath.Join(spoolDir, id), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
}

// overwriteChecked Lstats the destination and refuses a symlinked leaf
// before the write: the other leaf posture.
func overwriteChecked(spoolDir, id string, payload []byte) error {
	p := filepath.Join(spoolDir, id)
	fi, err := os.Lstat(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if fi != nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return os.WriteFile(p, payload, 0o600)
}

// lstatElsewhere: an Lstat (and a ModeSymlink consult) on an unrelated
// path checks no leaf of this write.
func lstatElsewhere(spoolDir, id string, payload []byte) error {
	fi, err := os.Lstat(filepath.Join(spoolDir, "sentinel"))
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("sentinel is a symlink")
	}
	return os.WriteFile(filepath.Join(spoolDir, id), payload, 0o600) // want `write under a root with lexical containment only`
}
