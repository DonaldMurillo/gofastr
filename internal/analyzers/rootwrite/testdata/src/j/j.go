// Package j pins the 2026-11 ordering fix in pathflow: a resolution
// posture counts only when it DOMINATES the sink. An EvalSymlinks, a
// guard call, or an Lstat leaf check that runs after the write — a
// verification read-back, a post-hoc audit — resolved nothing for the
// write that already happened, and used to quiet it from anywhere in
// the function.
package j

import (
	"errors"
	"os"
	"path/filepath"
)

// resolveAfterWrite verifies the path after writing it: the write
// itself was still lexically contained only.
func resolveAfterWrite(baseDir, name string, data []byte) error {
	p := filepath.Join(baseDir, "inbox", name)
	if err := os.WriteFile(p, data, 0o600); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	_ = real
	return nil
}

// evalOnDirAfterSink resolves the parent chain after the write: same
// ordering gap.
func evalOnDirAfterSink(baseDir, name string, data []byte) error {
	p := filepath.Join(baseDir, "inbox", name)
	if err := os.WriteFile(p, data, 0o600); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	if _, err := filepath.EvalSymlinks(filepath.Dir(p)); err != nil {
		return err
	}
	return nil
}

func auditLeaf(p string) error {
	fi, err := os.Lstat(p)
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink leaf")
	}
	return nil
}

// lstatAfterSink checks the leaf after clobbering it: the leaf posture
// must also precede the sink.
func lstatAfterSink(workDir, id string, data []byte) error {
	p := filepath.Join(workDir, id)
	if err := os.WriteFile(p, data, 0o600); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	return auditLeaf(p)
}

func refuseEscape(p string) error {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if real != p {
		return errors.New("escaped")
	}
	return nil
}

// guardAfterSink audits after the write: a guard after the fact gates
// nothing.
func guardAfterSink(workDir, id string, data []byte) error {
	p := filepath.Join(workDir, id)
	if err := os.WriteFile(p, data, 0o600); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	return refuseEscape(p)
}

// resolveInIfInit runs the EvalSymlinks in the if's Init, the write in
// its then-branch: the Init always ran first. Quiet.
func resolveInIfInit(baseDir, name string, data []byte) error {
	if real, err := filepath.EvalSymlinks(filepath.Join(baseDir, "inbox", name)); err == nil {
		return os.WriteFile(real, data, 0o600)
	}
	return nil
}

// resolveBeforeLoop resolves once, then writes inside the loop: the
// resolution dominates every iteration. Quiet.
func resolveBeforeLoop(baseDir string, names []string, data []byte) error {
	root, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, n), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// resolveInSiblingBranch runs the EvalSymlinks in a branch the write
// never shares: it resolves nothing, before or after.
func resolveInSiblingBranch(baseDir, name string, data []byte, audit bool) error {
	if audit {
		if _, err := filepath.EvalSymlinks(filepath.Join(baseDir, "inbox", name)); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(baseDir, "inbox", name), data, 0o600) // want `write under a root with lexical containment only`
}
