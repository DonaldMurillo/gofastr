package runtime

import (
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"sort"
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

// TestEmbedCompositionOmitsNav is the structural half of "SPA navigation is
// disabled inside frames". The design decision was to enforce it by ABSENCE —
// the nav fragment is not composed — rather than by a runtime flag, precisely
// so a later refactor cannot re-enable it by flipping a boolean. This asserts
// the absence is real: not one of nav.js's top-level symbols may appear in the
// embed bundle.
func TestEmbedCompositionOmitsNav(t *testing.T) {
	navSrc, err := fs.ReadFile(fragFS, "frag/nav.js")
	if err != nil {
		t.Fatalf("read nav fragment: %v", err)
	}
	navSymbols := declaredSymbols(string(navSrc))
	if len(navSymbols) == 0 {
		t.Fatal("no symbols parsed out of nav.js — the guard below would pass vacuously")
	}

	embedded, err := composeEmbed()
	if err != nil {
		t.Fatalf("composeEmbed: %v", err)
	}
	composed := declaredSymbols(embedded)

	var leaked []string
	for name := range navSymbols {
		if _, ok := composed[name]; ok {
			leaked = append(leaked, name)
		}
	}
	if len(leaked) > 0 {
		sort.Strings(leaked)
		t.Fatalf("the embed composition declares %d nav symbol(s) — SPA navigation is supposed to be impossible inside a frame BY ABSENCE:\n  %s",
			len(leaked), strings.Join(leaked, "\n  "))
	}

	// And the fragment that replaces it must be there, or the frame has no way
	// to receive its credential at all.
	if !strings.Contains(embedded, "gofastr-embed/1") {
		t.Error("the embed composition does not contain boot-embed's protocol marker")
	}
}

// TestBootToleratesAMissingNav pins the one cross-fragment call that made a
// nav-less composition possible. boot's _initialPass calls updateActiveLink,
// which lives in nav; without the typeof probe the embed bundle throws a
// ReferenceError at boot and takes hydration, module loading and the CSS
// scanner down with it — a failure that looks like "the embed renders nothing".
func TestBootToleratesAMissingNav(t *testing.T) {
	src, err := fs.ReadFile(fragFS, "frag/boot.js")
	if err != nil {
		t.Fatalf("read boot fragment: %v", err)
	}
	if !strings.Contains(string(src), "typeof updateActiveLink === 'function'") {
		t.Fatal("boot.js calls updateActiveLink without a typeof probe — the embed composition omits nav and would throw at boot")
	}
}

// TestCompositionsShareByteIdenticalFragments is the anti-drift guard for the
// composer as a whole. Every bundle is assembled from the SAME files by the
// same function, so a fix applied to one composition cannot fail to reach
// another. This asserts the assembly really is file concatenation — if a
// composition ever grew a per-bundle transform, the substring check below stops
// being true and the divergence surfaces here rather than in production.
func TestCompositionsShareByteIdenticalFragments(t *testing.T) {
	compositions := map[string][]string{
		"full":   fullFragmentOrder,
		"static": staticFragmentOrder,
		"embed":  embedFragmentOrder,
	}
	built := map[string]string{}
	var err error
	if built["full"], err = composeFull(); err != nil {
		t.Fatalf("composeFull: %v", err)
	}
	if built["static"], err = composeStatic(); err != nil {
		t.Fatalf("composeStatic: %v", err)
	}
	if built["embed"], err = composeEmbed(); err != nil {
		t.Fatalf("composeEmbed: %v", err)
	}

	for name, order := range compositions {
		for _, frag := range order {
			data, err := fs.ReadFile(fragFS, "frag/"+frag+".js")
			if err != nil {
				t.Fatalf("read fragment %q: %v", frag, err)
			}
			body := strings.TrimRight(string(data), "\n")
			if !strings.Contains(built[name], body) {
				t.Errorf("composition %q does not contain fragment %q verbatim — the bundles have started to diverge", name, frag)
			}
		}
	}

	// rpc and rpc-stub are mutually exclusive, as are widgets-boot and
	// widgets-boot-static. Composing both halves of either pair would install
	// two definitions of the same behaviour in one scope.
	exclusive := [][2]string{{"rpc", "rpc-stub"}, {"widgets-boot", "widgets-boot-static"}}
	for name, order := range compositions {
		has := map[string]bool{}
		for _, f := range order {
			has[f] = true
		}
		for _, pair := range exclusive {
			if has[pair[0]] && has[pair[1]] {
				t.Errorf("composition %q includes both %q and %q, which are mutually exclusive", name, pair[0], pair[1])
			}
		}
	}
}

// TestBootEmbedMeasuresContentNotViewport pins the height-report source.
//
// An embedded surface lives in an iframe the host page resizes to the height
// the frame reports. documentElement.scrollHeight is at least the viewport
// height, and that viewport IS the frame being resized — so measuring it feeds
// each report into the next measurement, and any full-height rule inside makes
// the panel ratchet open with a band of empty space under the content.
//
// This is a source-level check on purpose. The browser-level version is not
// worth having: by the time a test can observe the frame the ratchet has
// already settled, so two equal measurements prove nothing. What actually
// caught this was a screenshot, and what keeps it fixed is the pair of this
// check and TestEmbedLayoutIsNotViewportTall in core-ui/app.
func TestBootEmbedMeasuresContentNotViewport(t *testing.T) {
	src, err := fs.ReadFile(fragFS, "frag/boot-embed.js")
	if err != nil {
		t.Fatalf("read boot-embed fragment: %v", err)
	}
	body := string(src)
	// A denylist of four spellings was the previous shape of this gate, and it
	// could not see document.body.scrollHeight, root.scrollHeight,
	// visualViewport.height, getComputedStyle(docEl).height, or
	// observe(document.body) — any one of which reopens the ratchet. Assert what
	// the measurement IS instead: every height-shaped read in this fragment has
	// to come from the root's own bounding box.
	heightRead := regexp.MustCompile(`(?:scrollHeight|offsetHeight|clientHeight|innerHeight|visualViewport|getComputedStyle)`)
	if m := heightRead.FindAllString(body, -1); len(m) > 0 {
		t.Errorf("boot-embed reads %v — the height report must be a function of the CONTENT, "+
			"not of the frame it is resizing, so the only permitted source is "+
			"root.getBoundingClientRect()", m)
	}
	// And the observer must watch the root, never the document or the body:
	// observing either means observing the thing the report resizes.
	observe := regexp.MustCompile(`observe\(\s*([A-Za-z_.]+)`)
	for _, m := range observe.FindAllStringSubmatch(body, -1) {
		if m[1] != "root" {
			t.Errorf("boot-embed observes %q; observing anything but the root is a feedback loop", m[1])
		}
	}
	if !strings.Contains(body, "root.getBoundingClientRect()") {
		t.Error("boot-embed does not measure the embed root's own extent")
	}
}

// The catalog's stylePath may already carry a query — an embed frame appends
// ?t=<variant> so component CSS resolves under the same theme as app.css.
// Appending the version with a hard-coded '?' folded both into one unparseable
// parameter, so the server saw an unknown theme key and no version at all: the
// frame silently served the app palette with caching disabled. Nothing in Go
// can catch that; the defect is in this line of JavaScript.
func TestKernelAppendsVersionWithTheRightSeparator(t *testing.T) {
	src, err := fs.ReadFile(fragFS, "frag/kernel.js")
	if err != nil {
		t.Fatalf("read kernel fragment: %v", err)
	}
	body := string(src)

	naive := regexp.MustCompile(`stylePath\s*\+\s*\([^)]*'\?v='`)
	if naive.MatchString(body) {
		t.Error("kernel.js appends the version with a hard-coded '?', which breaks any " +
			"stylePath that already has a query — the embed frame's ?t=<variant> is one")
	}
	if !strings.Contains(body, `indexOf('?')`) {
		t.Error("kernel.js does not choose its query separator; a stylePath carrying a " +
			"query would get a second '?'")
	}
}
