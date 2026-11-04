// Package i pins the 2026-11 round's sink additions: os.Mkdir (the
// single-level twin the MkdirAll arm already had) and os.CreateTemp
// (its first argument, the caller-named directory the temp file lands
// in — the first half of the write-then-rename pattern the rule
// already covers). Both create under a root whose containment is
// lexical only.
package i

import (
	"os"
	"path/filepath"
)

// jobDir creates one caller-named directory under baseDir: a
// symlinked "jobs/<id>" component plants it outside baseDir.
func jobDir(baseDir, id string) error {
	return os.Mkdir(filepath.Join(baseDir, "jobs", id), 0o700) // want `write under a root with lexical containment only`
}

// stageFile writes a temp file inside a caller-named directory.
func stageFile(baseDir, name string) (*os.File, error) {
	return os.CreateTemp(filepath.Join(baseDir, "stage", name), "run-*") // want `write under a root with lexical containment only`
}

// jobDirSafe is the fix posture: resolve the chain before creating.
func jobDirSafe(baseDir, id string) error {
	p := filepath.Join(baseDir, "jobs", id)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	return os.Mkdir(real, 0o700)
}

// stageViaRoot is the strongest fix: kernel containment.
func stageViaRoot(baseDir, name string) (*os.File, error) {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Create(filepath.Join("stage", name, "run-1"))
}

// mkdirOfLiteral creates a fixed subdirectory of a root: nothing
// caller-controlled is appended, so the lexical containment is all
// there is to check.
func mkdirOfLiteral(root string) error {
	if err := os.Mkdir(filepath.Join(root, "manifests"), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

// createTempInTempRoot lands in a throwaway directory: silent.
func createTempInTempRoot() (*os.File, error) {
	tmp, err := os.MkdirTemp("", "stage-*")
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(tmp, "run-*")
}
