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

// relLongPrefix tests a prefix longer than "..": "../outside" passes.
func relLongPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || strings.HasPrefix(rel, "..blocked") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relParentOnly misses rel == "..", the root's parent itself.
func relParentOnly(baseDir, name string) (*os.File, error) {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || strings.HasPrefix(rel, "../") {
		return nil, errEscape
	}
	return os.CreateTemp(real, "upload-*") // want `write under a root with lexical containment only`
}

// relDotDotPrefix refuses every rel starting with "..". Quiet.
func relDotDotPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// relIsLocal refuses a rel that is not local. Quiet.
func relIsLocal(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// gotoWrite jumps straight to the write when the path is outside.
func gotoWrite(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		goto write
	}
write:
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// continueSkips skips an outside name and writes the inside ones.
// Quiet.
func continueSkips(baseDir string, names []string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
			continue
		}
		if err := os.WriteFile(real, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// breakOutToWrite breaks out of the loop and writes after it: the
// break reaches the write.
func breakOutToWrite(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	for {
		if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
			break
		}
		break
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// breakSwitchInLoop breaks out of the switch, not the loop: the write
// after the switch still runs for an outside name.
func breakSwitchInLoop(baseDir string, names []string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
		if err != nil {
			return err
		}
		switch {
		default:
			if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
				break
			}
		}
		if err := os.WriteFile(real, data, 0o600); err != nil { // want `write under a root with lexical containment only`
			return err
		}
	}
	return nil
}

// relWrongSeparator tests the other platform's separator: on Unix
// "../outside" is neither ".." nor under `..\`.
func relWrongSeparator(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relMixed checks each part on a different Rel result: /safe/sibling
// is outside /safe/root and passes both.
func relMixed(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(realRoot, real)
	otherRel, _ := filepath.Rel(filepath.Dir(realRoot), real)
	if rel == ".." || strings.HasPrefix(otherRel, "../") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relReassigned keeps an alias of the first Rel, then rebinds rel to
// a second: the two parts read two answers.
func relReassigned(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(filepath.Dir(realRoot), real)
	alias := rel
	rel, _ = filepath.Rel(realRoot, real)
	if rel == ".." || strings.HasPrefix(alias, ".."+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// checkBeforeResolve tests the path before EvalSymlinks rebinds it:
// the check read the unresolved spelling, so a link under the root
// passes it and the resolved path leads out.
func checkBeforeResolve(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	p := filepath.Join(realRoot, name)
	if !strings.HasPrefix(p, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// boolBeforeResolve keeps that early answer in a bool and reads it
// after the resolution: it still describes the unresolved spelling.
func boolBeforeResolve(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	p := filepath.Join(realRoot, name)
	inside := strings.HasPrefix(p, realRoot+string(filepath.Separator))
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if !inside {
		return errEscape
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// siblingPrefix has no separator after the root: /srv/app-backup
// passes a test against /srv/app.
func siblingPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// selfPrefix tests the resolved path against its own parent: true for
// nearly every path, wherever it points.
func selfPrefix(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, filepath.Dir(real)+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// swappedPrefix passes the operands the wrong way round: it asks
// whether the root starts with the target.
func swappedPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(realRoot+string(filepath.Separator), real) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// swappedRel computes Rel(real, root): the path from the target back
// to the root, which says nothing about containment.
func swappedRel(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(real, realRoot)
	if err != nil || !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// boundPrefix keeps root+separator in a local first. Quiet.
func boundPrefix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	rootSep := realRoot + string(filepath.Separator)
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, rootSep) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// rootPrefixOnly tests the root against itself and writes the target:
// the check names no path the write uses.
func rootPrefixOnly(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(baseDir, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// prefixWrongSuffix puts a constant other than the separator after the
// root: /srv/app_x passes a test against /srv/app + "_".
func prefixWrongSuffix(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+"_") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relUnresolved runs Rel on the unresolved join and writes the
// resolved path: the check never saw where the link points.
func relUnresolved(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, filepath.Join(realRoot, name))
	if err != nil || !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// transformedPrefix tests a path rebuilt from the resolved one: the
// Base of a link's target, joined under the root, passes wherever the
// link points.
func transformedPrefix(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.Join(baseDir, filepath.Base(real)), baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// transformedRel is the same rebuild under filepath.Rel.
func transformedRel(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseDir, filepath.Join(baseDir, filepath.Base(real)))
	if err != nil || !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// staleAlias checks a copy of the first resolution and writes the
// second.
func staleAlias(baseDir, safeName, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, safeName))
	if err != nil {
		return err
	}
	checked := real
	real, err = filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(checked, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// aliasAfter copies the resolved path once, after the resolution, and
// checks the copy. Quiet.
func aliasAfter(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	checked := real
	if !strings.HasPrefix(checked, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// constantAliasRoot hides an empty root in a local: "" + sep is "/",
// which every absolute path starts with.
func constantAliasRoot(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	root := ""
	if !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// aliasMaybe points the alias at the resolution only on one branch:
// on the other it still holds the safe name's resolution.
func aliasMaybe(baseDir, safeName, name string, data []byte, swap bool) error {
	checked, err := filepath.EvalSymlinks(filepath.Join(baseDir, safeName))
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if swap {
		checked = real
	}
	if !strings.HasPrefix(checked, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relParentRoot tests against the root's parent: a sibling of the root
// passes.
func relParentRoot(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(filepath.Dir(realRoot), real)
	if err != nil || !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// prefixParentRoot is the same widening through a prefix.
func prefixParentRoot(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, filepath.Dir(baseDir)+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// otherRoot tests against a directory the path was not joined under.
func otherRoot(baseDir, elsewhere, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, elsewhere+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// nestedJoinRoot joins in steps and tests against the outer root.
// Quiet.
func nestedJoinRoot(baseDir, id, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	dir := filepath.Join(baseDir, id)
	real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// parentThenMkdirAll is parentThenMkdir through MkdirAll, which stats
// a linked leaf, finds a directory and returns nil: the later write
// lands outside.
func parentThenMkdirAll(baseDir, name string) error {
	p := filepath.Join(baseDir, name)
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(dir, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.MkdirAll(filepath.Join(dir, filepath.Base(p)), 0o700) // want `write under a root with lexical containment only`
}

// constJoinHome names the root as Join(baseDir, ".app") and tests
// against baseDir: anything else under baseDir passes.
func constJoinHome(baseDir, name string, data []byte) error {
	root := filepath.Join(baseDir, ".app")
	real, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// constJoinRoot tests the same path against the root itself. Quiet.
func constJoinRoot(baseDir, name string, data []byte) error {
	root := filepath.Join(baseDir, ".app")
	real, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// relErrIgnored drops Rel's error: a failed Rel gives rel == "",
// which no ".." test catches.
func relErrIgnored(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(realRoot, real)
	if strings.HasPrefix(rel, "..") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relErrIgnoredPair drops it under the two-part test.
func relErrIgnoredPair(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(realRoot, real)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relErrSeparate refuses Rel's error in its own if first. Quiet.
func relErrSeparate(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rel, "..") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// relErrReplaced tests an err that no longer holds Rel's.
func relErrReplaced(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, real)
	_, err = os.Stat(realRoot)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rel, "..") {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// relIsLocalNoErr drops Rel's error under IsLocal, which is false for
// "". Quiet.
func relIsLocalNoErr(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(realRoot, real)
	if !filepath.IsLocal(rel) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// copySink checks the resolved path and writes a copy of it. Quiet.
func copySink(baseDir, name string, data []byte) error {
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
	out := real
	return os.WriteFile(out, data, 0o600)
}

// maybeResolved resolves only on one branch: on the other the check
// is lexical and the write follows the link.
func maybeResolved(baseDir, name string, data []byte, resolve bool) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	p := filepath.Join(realRoot, name)
	if resolve {
		p, err = filepath.EvalSymlinks(p)
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(p, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(p, data, 0o600) // want `write under a root with lexical containment only`
}

// concatJoin builds the path by concatenation and checks it against
// the root it was built under. Quiet.
func concatJoin(baseDir, name string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(baseDir + string(filepath.Separator) + name)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// concatParent is the same build checked against the root's parent.
func concatParent(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(baseDir + string(filepath.Separator) + name)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, filepath.Dir(baseDir)+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// staleCopySink writes a copy made before real was resolved again: the
// check reads the second resolution, the write holds the first.
func staleCopySink(baseDir, name, other string, data []byte) error {
	realRoot, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	out := real
	real, err = filepath.EvalSymlinks(filepath.Join(baseDir, other))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(out, data, 0o600) // want `write under a root with lexical containment only`
}

// sinkReassigned checks the first resolution and writes another one
// copied into the same local after the check.
func sinkReassigned(baseDir, safeName, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, safeName))
	if err != nil {
		return err
	}
	other, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	real = other
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// branchRoot empties the root on one branch: "" + sep is "/".
func branchRoot(baseDir, name string, data []byte, widen bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	root := baseDir
	if widen {
		root = ""
	} else {
		root = baseDir
	}
	if !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// paramRootWidened joins under baseDir, then widens baseDir to its
// parent on one branch before the check.
func paramRootWidened(baseDir, name string, data []byte, widen bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if widen {
		baseDir = filepath.Dir(baseDir)
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// parenConstRoot hides an empty root behind parentheses.
func parenConstRoot(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	root := ""
	if !strings.HasPrefix(real, (root)+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// checkedSourceAliasSink checks the resolution and writes a copy made
// once, after it. Quiet.
func checkedSourceAliasSink(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	alias := real
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(alias, data, 0o600)
}

// branchResolvedRoot resolves the two-valued root before the test.
func branchResolvedRoot(baseDir, name string, data []byte, widen bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	root := "/"
	if !widen {
		root = baseDir
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// condRootSep binds root+sep on one branch only: otherwise the prefix
// is "", and HasPrefix(real, "") holds for every path.
func condRootSep(baseDir, name string, data []byte, strict bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rootSep := ""
	if strict {
		rootSep = baseDir + string(filepath.Separator)
	}
	if !strings.HasPrefix(real, rootSep) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// boundRootSep binds root+sep once. Quiet.
func boundRootSep(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rootSep := baseDir + string(filepath.Separator)
	if !strings.HasPrefix(real, rootSep) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// rootConfig carries a field root for fieldRoot and safeField.
type rootConfig struct{ RootDir string }

// fieldRoot clears the field root between the join and the check: the
// prefix becomes the separator alone.
func fieldRoot(settings *rootConfig, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(settings.RootDir, name))
	if err != nil {
		return err
	}
	settings.RootDir = ""
	if !strings.HasPrefix(real, settings.RootDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// safeField checks against the same field, never written. Quiet.
func safeField(settings *rootConfig, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(settings.RootDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, settings.RootDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// pointerRoot clears the root through a pointer, which no binding shows.
func pointerRoot(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	rootPointer := &baseDir
	*rootPointer = ""
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// closureRoot clears the root inside a closure called before the check.
func closureRoot(baseDir, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	widen := func() { baseDir = "" }
	widen()
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// parenthesizedRoot parenthesizes the root at the join and the check,
// so both key as the parameter, and widens it on one branch.
func parenthesizedRoot(baseDir, name string, data []byte, widen bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join((baseDir), name))
	if err != nil {
		return err
	}
	if widen {
		baseDir = ""
	}
	if !strings.HasPrefix(real, (baseDir)+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// pointerSink checks real, then replaces it through a pointer with an
// unchecked resolution.
func pointerSink(baseDir, safeName, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, safeName))
	if err != nil {
		return err
	}
	other, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	destination := &real
	*destination = other
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// frozenRoot checks against a copy of the root; the root changes only
// after the check, which changes neither string. Quiet.
func frozenRoot(baseDir, name, nextDir string, data []byte, unused bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	frozen := baseDir
	if !strings.HasPrefix(real, frozen+string(filepath.Separator)) {
		return errEscape
	}
	if unused {
		baseDir = nextDir
	}
	return os.WriteFile(real, data, 0o600)
}

// staleIntermediate copies the first resolution, resolves again, then
// copies the stale copy after the second call: out holds the first.
func staleIntermediate(baseDir, safeName, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	stale := real
	real, err = filepath.EvalSymlinks(filepath.Join(baseDir, safeName))
	if err != nil {
		return err
	}
	out := stale
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(out, data, 0o600) // want `write under a root with lexical containment only`
}

// frozenBeforeWiden copies the root, then widens the source before the
// check: the copy still holds the root the path was joined under. Quiet.
func frozenBeforeWiden(baseDir, name string, data []byte, widen bool) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	frozen := baseDir
	if widen {
		baseDir = ""
	}
	if !strings.HasPrefix(real, frozen+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}

// fieldPointerRoot clears the field root through a pointer to it.
func fieldPointerRoot(settings *rootConfig, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(settings.RootDir, name))
	if err != nil {
		return err
	}
	p := &settings.RootDir
	*p = ""
	if !strings.HasPrefix(real, settings.RootDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// fieldBaseSwapped points the struct at another config before the check.
func fieldBaseSwapped(settings, other *rootConfig, name string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(settings.RootDir, name))
	if err != nil {
		return err
	}
	settings = other
	if !strings.HasPrefix(real, settings.RootDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// rangeRoot rebinds the root in a range clause before the check.
func rangeRoot(baseDir, name string, alts []string, data []byte) error {
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	for _, baseDir = range alts {
	}
	if !strings.HasPrefix(real, baseDir+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// zeroLocalRoot checks against a local that is still "" at the check;
// it is bound to the root only afterwards.
func zeroLocalRoot(baseDir, name string, data []byte) error {
	var root string
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return errEscape
	}
	root = baseDir
	_ = root
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// frozenBeforeRebind copies the root, rebinds it, and joins under the
// new value: the check tests the old root.
func frozenBeforeRebind(baseDir, name, nextDir string, data []byte) error {
	frozen := baseDir
	baseDir = nextDir
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, frozen+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600) // want `write under a root with lexical containment only`
}

// frozenBeforeJoin copies the root before the join, and nothing
// rebinds it. Quiet.
func frozenBeforeJoin(baseDir, name string, data []byte) error {
	frozen := baseDir
	real, err := filepath.EvalSymlinks(filepath.Join(baseDir, name))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, frozen+string(filepath.Separator)) {
		return errEscape
	}
	return os.WriteFile(real, data, 0o600)
}
