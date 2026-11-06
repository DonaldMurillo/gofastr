// Package d pins the 2026-11 ordering fix in pathflow on the read
// side: an EvalSymlinks or a guard that runs after the read resolved
// nothing for it, and used to quiet it from anywhere in the function.
package d

import (
	"errors"
	"os"
	"path/filepath"
)

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

// resolveAfterRead audits the path after disclosing it: the open was
// still lexically contained only.
func resolveAfterRead(baseDir, name string) ([]byte, error) {
	p := filepath.Join(baseDir, "inbox", name)
	data, err := os.ReadFile(p) // want `read under a root with lexical containment only`
	if err != nil {
		return nil, err
	}
	if err := refuseEscape(p); err != nil {
		return nil, err
	}
	return data, nil
}

// guardAfterStat stats first, audits second.
func guardAfterStat(baseDir, name string) (os.FileInfo, error) {
	p := filepath.Join(baseDir, "inbox", name)
	fi, err := os.Stat(p) // want `read under a root with lexical containment only`
	if err != nil {
		return nil, err
	}
	if err := refuseEscape(p); err != nil {
		return nil, err
	}
	return fi, nil
}

// resolveBeforeRead is the fix posture: quiet.
func resolveBeforeRead(baseDir, name string) ([]byte, error) {
	p := filepath.Join(baseDir, "inbox", name)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(real)
}
