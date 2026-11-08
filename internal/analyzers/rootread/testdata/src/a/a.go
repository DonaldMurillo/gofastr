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

// resolveAuditsOther resolves another ref's chain inside the helper:
// the returned path's own chain is unchecked, and a body-level
// EvalSymlinks used to shield it (2026-11 tie fix).
func (w *warehouse) resolveAuditsOther(ref, other string) (string, error) {
	if _, err := filepath.EvalSymlinks(filepath.Join(w.shelfRoot, other)); err != nil {
		return "", err
	}
	return filepath.Join(w.shelfRoot, ref), nil
}

// loadViaAuditingHelper reads through it: must fire.
func (w *warehouse) loadViaAuditingHelper(ref, other string) ([]byte, error) {
	p, err := w.resolveAuditsOther(ref, other)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p) // want `read under a root with lexical containment only`
}

// fetchPage is a read helper: its parameter reaches os.ReadFile
// unresolved — until the 2026-11 hop port, a root-joined path handed
// to it was invisible (rootwrite already had this hop for writes).
func fetchPage(src string) ([]byte, error) {
	return os.ReadFile(src)
}

// loadViaReadHelper hands a root-joined path to it: must fire.
func (w *warehouse) loadViaReadHelper(ref string) ([]byte, error) {
	return fetchPage(filepath.Join(w.shelfRoot, ref)) // want `read under a root with lexical containment only`
}

// dropFile is the Remove spelling one helper deep.
func dropFile(target string) error {
	return os.Remove(target)
}

func (w *warehouse) dropViaHelper(ref string) error {
	return dropFile(filepath.Join(w.shelfRoot, ref)) // want `read under a root with lexical containment only`
}

// fetchPageResolved reads the resolved path with no containment check
// (#665): its callers fire.
func fetchPageResolved(src string) ([]byte, error) {
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(real)
}

func (w *warehouse) loadViaResolvedReadHelper(ref string) ([]byte, error) {
	return fetchPageResolved(filepath.Join(w.shelfRoot, ref)) // want `read under a root with lexical containment only`
}

// manifestViaHelper: a helper called with nothing caller-controlled
// stays quiet.
func (w *warehouse) manifestViaHelper() ([]byte, error) {
	return fetchPage(filepath.Join(w.shelfRoot, "manifest.json"))
}

// loadFixed checks the resolved directory, then opens the original
// path: a link swapped in after the check wins (#665).
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
	return os.Open(full) // want `read under a root with lexical containment only`
}

// resolveResolved checks the resolved path but returns the original
// spelling (#665).
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

// loadViaResolvedHelper reads resolveResolved's unpinned spelling.
func (w *warehouse) loadViaResolvedHelper(ref string) ([]byte, error) {
	p, err := w.resolveResolved(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p) // want `read under a root with lexical containment only`
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

func (w *warehouse) dropVarChecked(ref string) error {
	p, err := w.resolve(ref)
	if err != nil {
		return err
	}
	var info, statErr = os.Lstat(p)
	if statErr != nil {
		return statErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink at target")
	}
	return os.Remove(p)
}

// openNoFollow refuses the leaf symlink in the flags themselves.
func openNoFollow(scratchDir, id string) (*os.File, error) {
	return os.OpenFile(filepath.Join(scratchDir, id), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// localWriteFlags carries the write bits in a local: inline (rewrite
// above) this open belongs to rootwrite; through a local binding it
// must not become rootread's business either — the flag expression
// has to resolve through the binding before it is read.
func localWriteFlags(scratchDir, id string) (*os.File, error) {
	flags := os.O_RDWR | os.O_CREATE
	return os.OpenFile(filepath.Join(scratchDir, id), flags, 0o600)
}

// localNoFollow is a read open through a local: O_NOFOLLOW keeps the
// leaf posture quiet through a binding too.
func localNoFollow(scratchDir, id string) (*os.File, error) {
	flags := os.O_RDONLY | syscall.O_NOFOLLOW
	return os.OpenFile(filepath.Join(scratchDir, id), flags, 0)
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

// guardNamedSymlink was the quiet fixture bug bash 2026-10-27 I13
// named: the guard's body is `return nil` and its argument is the root
// alone. A name is not a resolution, and a root-only check leaves every
// component under it unchecked; it fires now. The real-guard spelling
// lives in package g.
func (w *warehouse) guardNamedSymlink(ref string) ([]byte, error) {
	if err := ensureNoSymlinkPath(w.shelfRoot); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(w.shelfRoot, scrubbed(ref))) // want `read under a root with lexical containment only`
}

func ensureNoSymlinkPath(string) error { return nil }

func scrubbed(ref string) string { return ref }
