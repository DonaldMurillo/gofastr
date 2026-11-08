// Package d pins the 2026-11 ordering fix in pathflow on the read
// side: an EvalSymlinks or a guard that runs after the read resolved
// nothing for it, and used to quiet it from anywhere in the function.
package d

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// resolveBeforeRead reads the resolved path with no containment
// check: it returned an outside file's bytes in the #665 replay.
func resolveBeforeRead(baseDir, name string) ([]byte, error) {
	p := filepath.Join(baseDir, "inbox", name)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(real) // want `read under a root with lexical containment only`
}

// walkUpward stats name in workingDir and each parent: the walk leaves
// the directory on purpose, and `dir = filepath.Dir(dir)` is not the
// self-rebind PathBindings unrolls. Quiet.
func walkUpward(workingDir, name string) []string {
	var out []string
	dir := workingDir
	for {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
		if dir == filepath.Dir(dir) {
			return out
		}
		dir = filepath.Dir(dir)
	}
}

// readBackwards returns early on an inside path and reads the outside
// one.
func readBackwards(baseDir, name string) ([]byte, error) {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return nil, nil
	}
	return os.ReadFile(real) // want `read under a root with lexical containment only`
}
