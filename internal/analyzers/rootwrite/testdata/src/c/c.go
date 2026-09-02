// Package c is a third rootwrite positive with a different layout: the
// root is a struct field reached through the receiver, with the fixed
// spelling resolving symlinks inline.
package c

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type vault struct {
	root string
}

var errEscape = errors.New("path escapes vault root")

// store writes caller-named blobs under v.root with a Join alone.
func (v *vault) store(token string, data []byte) error {
	return os.WriteFile(filepath.Join(v.root, token), data, 0o600) // want `write under a root with lexical containment only`
}

// storeSafe is the fix posture: resolve symlinks before the write.
func (v *vault) storeSafe(token string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(v.root)
	if err != nil {
		return err
	}
	p := filepath.Join(v.root, token)
	real, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(os.PathSeparator)) {
		return errEscape
	}
	return os.WriteFile(p, data, 0o600)
}

// mkdirAllSafe resolves once for the whole tree it is about to create.
func (v *vault) mkdirAllSafe(rel string) error {
	realRoot, err := filepath.EvalSymlinks(v.root)
	if err != nil {
		return err
	}
	realRel, err := filepath.EvalSymlinks(filepath.Join(v.root, rel))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(realRel, realRoot+string(os.PathSeparator)) {
		return errEscape
	}
	return os.MkdirAll(realRel, 0o755)
}
