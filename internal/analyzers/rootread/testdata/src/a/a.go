// Package a holds rootread fixtures reduced from the probe sites —
// core/upload local.go (Get/GetRange/Exists/Delete joining baseDir and
// a sanitized key, probe TestLocalStorageRefusesSymlinkEscape) and the
// helper-returned-path spelling of battery/storage — under entirely
// different identifiers, with the fix postures beside them.
package a

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// warehouse is the storage shape: caller-named objects under a root
// field, sanitized lexically before every operation.
type warehouse struct {
	shelfRoot string
}

func scrubRef(ref string) (string, error) {
	if strings.Contains(ref, "..") {
		return "", errors.New("traversal")
	}
	cleaned := filepath.Clean(ref)
	if cleaned == "." {
		return "", errors.New("empty")
	}
	return cleaned, nil
}

// load is the Get shape: sanitize, join, prefix-check, open. None of
// that sees a symlinked directory component.
func (w *warehouse) load(ref string) (*os.File, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(w.shelfRoot, safe)
	if !strings.HasPrefix(full, w.shelfRoot+string(os.PathSeparator)) {
		return nil, errors.New("escapes")
	}
	return os.Open(full) // want `read under a root with lexical containment only`
}

// probe is the Exists shape.
func (w *warehouse) probe(ref string) (bool, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(w.shelfRoot, safe)) // want `read under a root with lexical containment only`
	if err != nil {
		return false, nil
	}
	return true, nil
}

// drop is the Delete shape: an Lstat-less unlink through whatever the
// join produced.
func (w *warehouse) drop(ref string) error {
	safe, err := scrubRef(ref)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(w.shelfRoot, safe)) // want `read under a root with lexical containment only`
}

// slurp is the ReadFile shape with a flat concatenation.
func (w *warehouse) slurp(ref string) ([]byte, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(w.shelfRoot + "/" + safe) // want `read under a root with lexical containment only`
}

// resolve is the helper-returned-path spelling: the Join lives here.
func (w *warehouse) resolve(ref string) (string, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(w.shelfRoot, safe), nil
}

// loadViaHelper acts only on the helper's result.
func (w *warehouse) loadViaHelper(ref string) (*os.File, error) {
	p, err := w.resolve(ref)
	if err != nil {
		return nil, err
	}
	return os.Open(p) // want `read under a root with lexical containment only`
}

// loadFixed resolves the directory chain before opening — the fix
// posture, sanitizer and all.
func (w *warehouse) loadFixed(ref string) (*os.File, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(w.shelfRoot, safe)
	realRoot, err := filepath.EvalSymlinks(w.shelfRoot)
	if err != nil {
		return nil, err
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(realDir, realRoot+string(os.PathSeparator)) {
		return nil, errors.New("escapes through symlink")
	}
	return os.Open(full)
}

// resolveResolved is the fix posture at the helper: everything that
// flows through it was resolved before the caller touches it.
func (w *warehouse) resolveResolved(ref string) (string, error) {
	safe, err := scrubRef(ref)
	if err != nil {
		return "", err
	}
	p := filepath.Join(w.shelfRoot, safe)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(real, w.shelfRoot+string(os.PathSeparator)) {
		return "", errors.New("escapes")
	}
	return p, nil
}

// loadViaResolvedHelper reads a helper-produced path whose chain the
// helper already resolved.
func (w *warehouse) loadViaResolvedHelper(ref string) ([]byte, error) {
	p, err := w.resolveResolved(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// dropChecked Lstats the target and refuses a symlinked leaf before
// unlinking.
func (w *warehouse) dropChecked(ref string) error {
	p, err := w.resolve(ref)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return os.Remove(p)
}

// openNoFollow refuses the leaf symlink in the flags themselves.
func openNoFollow(scratchDir, id string) (*os.File, error) {
	return os.OpenFile(filepath.Join(scratchDir, id), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// openReadOnlyInsideWriter is not a read of this family's concern when
// the open carries write flags: rootwrite owns it.
func (w *warehouse) rewrite(ref string) (*os.File, error) {
	return os.OpenFile(filepath.Join(w.shelfRoot, scrubbed(ref)), os.O_RDWR, 0)
}

// literalNameOnly reads a fixed manifest: nothing caller-controlled
// under the root.
func (w *warehouse) literalNameOnly() ([]byte, error) {
	return os.ReadFile(filepath.Join(w.shelfRoot, "manifest.json"))
}

// constantRoot joins under a literal: no caller-controlled boundary.
func constantRoot(label string) ([]byte, error) {
	return os.ReadFile(filepath.Join("gen/sdk/dist", label))
}

// throwawayRoot reads inside a MkdirTemp tree.
func throwawayRoot(name string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "stage")
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, name))
}

// loadViaRoot reads through an os.Root: containment is enforced in
// the kernel — a symlink under the root cannot lead the read out, and
// there is no TOCTOU window between check and use. The strongest fix
// posture on Go 1.27; Root methods are no os.* sink and no fs.FS
// value here, so the rule is quiet by construction. load above is the
// same join through os.Open, and it fires.
func loadViaRoot(storeDir, sub, name string) ([]byte, error) {
	rt, err := os.OpenRoot(storeDir)
	if err != nil {
		return nil, err
	}
	defer rt.Close()
	return rt.ReadFile(filepath.Join(sub, name))
}

// peekViaRoot covers the remaining Root read spellings: Open and Stat
// on a joined name.
func peekViaRoot(storeDir, sub, name string) error {
	rt, err := os.OpenRoot(storeDir)
	if err != nil {
		return err
	}
	defer rt.Close()
	if _, err := rt.Stat(filepath.Join(sub, name)); err != nil {
		return err
	}
	f, err := rt.Open(filepath.Join(sub, name))
	if err != nil {
		return err
	}
	return f.Close()
}

// dropViaRoot unlinks through the kernel-contained root.
func dropViaRoot(storeDir, sub, name string) error {
	rt, err := os.OpenRoot(storeDir)
	if err != nil {
		return err
	}
	defer rt.Close()
	return rt.Remove(filepath.Join(sub, name))
}

// guardNamedSymlink spells the refusal without filepath.EvalSymlinks.
func (w *warehouse) guardNamedSymlink(ref string) ([]byte, error) {
	if err := ensureNoSymlinkPath(w.shelfRoot); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(w.shelfRoot, scrubbed(ref)))
}

func ensureNoSymlinkPath(string) error { return nil }

func scrubbed(ref string) string { return ref }
