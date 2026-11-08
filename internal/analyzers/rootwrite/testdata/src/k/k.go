// Package k pins issue #665: filepath.EvalSymlinks says where a path
// points and confines nothing. A resolution counts only when the sink
// goes through the resolved path AND a containment check of that
// resolved path against the resolved root dominates the sink; os.Root
// stays the fix of first resort. Contained and ViaRoot are exported so
// k_test.go can replay a symlink escape against them.
package k

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errEscape = errors.New("escapes the root")

// Contained resolves the target, checks it with filepath.Rel against
// the resolved root, and writes the resolved path. Quiet.
func Contained(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// containedPrefix is the strings.HasPrefix spelling. Quiet.
func containedPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// ViaRoot is kernel containment. Quiet by construction.
func ViaRoot(baseDir, name string, data []byte) error {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(name, data, 0o600)
}

// discardedRel computes the containment answer and never reads it.
func discardedRel(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	_, _ = filepath.Rel(baseDir, real)
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// checkAfterWrite compares only after the write already happened.
func checkAfterWrite(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if err := os.WriteFile(real, data, 0o600); err != nil { // want `write under a root with lexical containment only`
		return err
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return nil
}

// checkOther contains one resolved path and writes another.
func checkOther(baseDir, name, other string, data []byte) error {
	realOther, err := filepath.EvalSymlinks(filepath.Join(baseDir, other))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(realOther, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// parentThenWrite checks the resolved parent and writes the leaf under
// it: a symlinked leaf still leads the write out.
func parentThenWrite(baseDir, name string, data []byte) error {
	p := filepath.Join(baseDir, name)
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(dir, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(p)), data, 0o600) // want `write under a root with lexical containment only`
}

// parentThenMkdir checks the resolved parent and makes the leaf under
// it: mkdir refuses an existing leaf, symlink or not. Quiet.
func parentThenMkdir(baseDir, name string) error {
	p := filepath.Join(baseDir, name)
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(dir, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.Mkdir(filepath.Join(dir, filepath.Base(p)), 0o700)
}

// parentUncheckedMkdir resolves the parent and checks nothing.
func parentUncheckedMkdir(baseDir, name string) error {
	p := filepath.Join(baseDir, name)
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	return os.Mkdir(filepath.Join(dir, filepath.Base(p)), 0o700) // want `write under a root with lexical containment only`
}

type vault struct{ root string }

// locate returns the contained resolved path. Its callers are quiet.
func (v *vault) locate(name string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(v.root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(v.root, name))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", errEscape
	}
	return real, nil
}

func (v *vault) put(name string, data []byte) error {
	p, err := v.locate(name)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// baseLeafWrite joins a parameter's Base under the root: a fresh root
// join, not a re-join under a resolved parent. A linked leaf leads out.
func baseLeafWrite(baseDir, name string, data []byte) error {
	return os.WriteFile(filepath.Join(baseDir, filepath.Base(name)), data, 0o600) // want `write under a root with lexical containment only`
}

// baseLeafLocal is the same through locals.
func baseLeafLocal(baseDir, name string) error {
	leaf := filepath.Base(name)
	p := filepath.Join(baseDir, leaf)
	return os.Remove(p) // want `write under a root with lexical containment only`
}

// parentThenCreateTemp: CreateTemp's argument is the directory the file
// lands in, so a linked Base(p) leads it out despite the checked parent.
func parentThenCreateTemp(baseDir, name string) (*os.File, error) {
	p := filepath.Join(baseDir, name)
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(dir, baseDir+string(filepath.Separator)) {
		return nil, errEscape
	}
	return os.CreateTemp(filepath.Join(dir, filepath.Base(p)), "x*") // want `write under a root with lexical containment only`
}

// selfRebind resolves p in place and never compares it with the root.
func selfRebind(baseDir, name string, data []byte) error {
	p := filepath.Join(baseDir, name)
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// selfRebindContained resolves in place and checks it. Quiet.
func selfRebindContained(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	p := filepath.Join(baseDir, name)
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(p, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(p, data, 0o600)
}

// logOnlyCheck tests containment and only notes the answer.
func logOnlyCheck(baseDir, name string, data []byte, note func(string)) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		note("outside")
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// writeInsideCheck writes in the branch the check allowed. Quiet.
func writeInsideCheck(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return os.WriteFile(real, data, 0o600)
	}
	return errEscape
}

// containedBool keeps the containment answer in a bool before the if:
// the same check as containedPrefix. Quiet.
func containedBool(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	inside := strings.HasPrefix(real, realRoot+string(filepath.Separator))
	if !inside {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// containedCutPrefix reads CutPrefix's found result. Quiet.
func containedCutPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	_, inside := strings.CutPrefix(real, realRoot+string(filepath.Separator))
	if !inside {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// constantPrefix compares the resolved path with "/": every absolute
// path passes, and no root is named.
func constantPrefix(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, "/") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relErrOnly checks only Rel's error: Rel succeeds for "../outside".
func relErrOnly(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	_, relErr := filepath.Rel(realRoot, real)
	if relErr != nil {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// optionalCheck refuses only when check is set: the read of the
// containment answer does not dominate the write.
func optionalCheck(baseDir, name string, data []byte, check bool) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	inside := strings.HasPrefix(real, realRoot+string(filepath.Separator))
	if check {
		if !inside {
			return errEscape
		}
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// varRebind seeds p with a var spec, then resolves it in place and
// writes without a containment check.
func varRebind(baseDir, name string, data []byte) error {
	var p = filepath.Join(baseDir, name)
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// appendLeaf rebinds p to a Join of itself: the walk back to the root
// must end instead of recursing forever.
func appendLeaf(baseDir, name string, data []byte) error {
	p := filepath.Join(baseDir, name)
	p = filepath.Join(p, filepath.Base(name))
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// appendLeafParam rebinds a parameter to a Join of itself: nothing
// seeds its prior value, so the binding stays cyclic and the walk's
// shared budget is what ends it.
func appendLeafParam(baseDir, name string, data []byte) error {
	baseDir = filepath.Join(baseDir, filepath.Base(name))
	return os.WriteFile(baseDir, data, 0o600) // want `write under a root with lexical containment only`
}

// backwardsFallthrough returns early when the path is inside and
// writes when it is outside: the outside file was overwritten in the
// review's replay.
func backwardsFallthrough(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return nil
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// negatedHolds writes inside the branch an outside path takes.
func negatedHolds(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
	}
	return nil
}

// boolInsideReturn is backwardsFallthrough through a bool.
func boolInsideReturn(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	inside := strings.HasPrefix(real, realRoot+string(filepath.Separator))
	if inside {
		return nil
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// elseRefuses refuses in the else-branch an outside path takes. Quiet.
func elseRefuses(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		data = append(data, '\n')
	} else {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// insideOnlyWhenFlag refuses outside paths only when strict is set:
// `strict && !inside` is false for every path when strict is not.
func insideOnlyWhenFlag(baseDir, name string, data []byte, strict bool) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if strict && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}
