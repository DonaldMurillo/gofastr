package runtime

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// fragmentSymbolDecl matches a top-level (IIFE-body-level) function/const/let
// declaration inside the runtime IIFE. Indent is exactly two spaces — the
// indent of every declaration that lives directly inside `(() => { … })`.
// Deeper-indented declarations (block-scoped helpers like _kilnOK inside the
// delegator `if`, or consts inside a function body) are intentionally NOT
// matched: they are not part of the IIFE's top-level symbol table and are not
// what the spec's "every top-level symbol" bar protects against.
//
// Method shorthand inside the namespace literal (`navigate(p){…}`) is also not
// matched — it is an object property, not a function/const/let declaration.
// The namespace members are guarded separately by the attr/doc parity gate.
var fragmentSymbolDecl = regexp.MustCompile(
	`(?m)^  (?:async\s+)?(?:function|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`,
)

// declaredSymbols returns the set of top-level IIFE-body declaration names
// found in src. This is the "symbol table" the symbol-completeness gate
// compares between the pre-split runtime.js and the composed output.
func declaredSymbols(src string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range fragmentSymbolDecl.FindAllStringSubmatch(src, -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

// preSplitRuntimeJS returns the runtime.js source at HEAD (before the step-2
// extraction), via `git show`. Used as the symbol-completeness reference — it
// is the authority on which top-level symbols the runtime must still declare
// after the split, so silent code loss during extraction fails the build.
//
// `git show` is read-only and never touches the working tree (the brief
// forbids git checkout/restore/stash). If git is unavailable (e.g. a tarball
// export with no .git), the test skips rather than failing — the bar is
// meaningless without the pre-split reference.
func preSplitRuntimeJS(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "show", "HEAD:core-ui/runtime/runtime.js").Output()
	if err != nil {
		t.Skipf("git show HEAD:core-ui/runtime/runtime.js failed (%v); "+
			"cannot run symbol-completeness gate without the pre-split reference", err)
	}
	return string(out)
}

// TestComposedRuntimeIsSymbolComplete is acceptance-bar item 2 of the
// runtime-composer spec (step 2). It guards against silent code loss during
// fragment extraction: every top-level function/const/let declared in the
// pre-split runtime.js MUST also be declared in the composed `full` output.
//
// Byte-identity was the original bar but is impossible (the spec's corrected
// acceptance section explains why: fragments are not contiguous byte ranges,
// and the namespace literal is restructured into incremental Object.assign
// assembly). Symbol completeness is the stronger check where it actually
// matters — it catches the specific failure byte-identity was a proxy for
// (a function or const silently vanishing during the move into fragments).
//
// Extras (symbols in the composition but not in HEAD) are reported but do not
// fail: the step-2 split may legitimately introduce a small number of new
// IIFE-level helpers, and the bar is "nothing was lost", not "nothing was added".
//
// knownRemovals lists top-level symbols that were DELIBERATELY removed from
// the runtime AFTER the step-2 extraction. Each has a comment explaining why.
// Adding to this set is a design decision, not a convenience — it widens the
// "code was silently lost" guard, so the bar for entry is the same as the bar
// for editing a checked-in artifact: the removal must be intentional and
// documented in the fragment that lost the symbol.
var knownRemovals = map[string]bool{
	// Step 3 (static composition): _staticMode was the runtime branch that
	// tested <html data-fui-static> at request time. Composition replaced it
	// with build-time fragment selection — the `static` bundle omits the
	// fragments that branched on it, and the `full` bundle never carries the
	// marker, so the const and its branches are dead code in both. Removed
	// from kernel.js / rpc.js / widgets-boot.js.
	"_staticMode": true,
}

func TestComposedRuntimeIsSymbolComplete(t *testing.T) {
	raw, err := composeFull()
	if err != nil {
		t.Fatalf("composeFull: %v", err)
	}
	composed := declaredSymbols(raw)
	head := declaredSymbols(preSplitRuntimeJS(t))

	t.Logf("symbol sets: HEAD=%d  composed(full)=%d", len(head), len(composed))

	var missing []string
	for name := range head {
		if _, ok := composed[name]; !ok {
			if knownRemovals[name] {
				continue
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// Sort for deterministic output.
		// (maps iterate in random order; a sorted list is the difference
		// between a readable failure and a shuffled one.)
		for i := 1; i < len(missing); i++ {
			for j := i; j > 0 && missing[j-1] > missing[j]; j-- {
				missing[j-1], missing[j] = missing[j], missing[j-1]
			}
		}
		t.Fatalf("%d top-level symbol(s) declared in HEAD runtime.js are ABSENT from "+
			"the composed `full` runtime — code was silently lost during fragment "+
			"extraction:\n  %s\nRestore each into the owning fragment under "+
			"core-ui/runtime/frag/.", len(missing), strings.Join(missing, "\n  "))
	}

	// Report extras as information only. Step 2 may introduce new IIFE-level
	// helpers (e.g. a namespace alias); those are not regressions.
	var extra []string
	for name := range composed {
		if _, ok := head[name]; !ok {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		for i := 1; i < len(extra); i++ {
			for j := i; j > 0 && extra[j-1] > extra[j]; j-- {
				extra[j-1], extra[j] = extra[j], extra[j-1]
			}
		}
		t.Logf("note: %d symbol(s) in composed `full` are NEW vs HEAD (allowed; "+
			"introduced by the split): %s", len(extra), strings.Join(extra, ", "))
	}
}

// TestComposedRuntimeMatchesOnDiskFile is the drift guard. The gate tests in
// attrdoc_test.go / integrity_test.go scan `runtime.js` on DISK (os.ReadFile),
// while RuntimeJS() serves the Go composition (composeFull → minify). If the
// two drift apart, the gates would be certifying content users never receive.
//
// This test fails the moment a fragment file is edited without re-assembling
// runtime.js — keeping the on-disk canonical form honest with the actual
// composition the binary ships.
func TestComposedRuntimeMatchesOnDiskFile(t *testing.T) {
	raw, err := composeFull()
	if err != nil {
		t.Fatalf("composeFull: %v", err)
	}
	onDisk, err := os.ReadFile("runtime.js")
	if err != nil {
		t.Fatalf("read runtime.js: %v", err)
	}
	if raw == string(onDisk) {
		return
	}
	// Regenerate on request. Without this the only instruction a developer who
	// edited a fragment gets is "re-assemble it somehow" — there is no
	// generator, no go:generate, and hand-copying the composition is absurd.
	// The env var keeps the default behaviour a hard failure (CI must never
	// silently rewrite a checked-in artifact) while giving the fix a name.
	if os.Getenv("GOFASTR_UPDATE_RUNTIME_JS") != "" {
		if err := os.WriteFile("runtime.js", []byte(raw), 0o644); err != nil {
			t.Fatalf("rewrite runtime.js: %v", err)
		}
		t.Logf("runtime.js regenerated from frag/*.js (%d bytes) — re-run without "+
			"GOFASTR_UPDATE_RUNTIME_JS to verify.", len(raw))
		return
	}
	t.Errorf("composeFull() output does NOT match on-disk runtime.js — the gate "+
		"tests scan a file that drifted from the served composition.\n"+
		"  composeFull: %d bytes\n  runtime.js: %d bytes\n"+
		"runtime.js is GENERATED from frag/*.js; edit the fragments, not this file.\n"+
		"Regenerate with:\n"+
		"  GOFASTR_UPDATE_RUNTIME_JS=1 go test ./core-ui/runtime/ -run TestComposedRuntimeMatchesOnDiskFile",
		len(raw), len(onDisk))
}
