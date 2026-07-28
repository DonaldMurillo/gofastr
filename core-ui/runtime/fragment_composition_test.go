package runtime

import (
	"io/fs"
	"os"
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

// symbolManifestPath is the checked-in reference for the symbol-completeness
// gate below.
const symbolManifestPath = "frag/SYMBOLS.txt"

// fragmentSymbolIndex renders the manifest form of what frag/*.js declares
// right now: one "<fragment>\t<symbol>" line per top-level declaration,
// sorted. Every fragment is covered, not just the ones the `full`
// composition uses — losing a symbol out of rpc-stub or boot-embed breaks
// the static / embed bundles just as badly.
func fragmentSymbolIndex(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(fragFS, "frag")
	if err != nil {
		t.Fatalf("read frag dir: %v", err)
	}
	var lines []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		src, err := fs.ReadFile(fragFS, "frag/"+e.Name())
		if err != nil {
			t.Fatalf("read fragment %q: %v", e.Name(), err)
		}
		frag := strings.TrimSuffix(e.Name(), ".js")
		for name := range declaredSymbols(string(src)) {
			lines = append(lines, frag+"\t"+name)
		}
	}
	sort.Strings(lines)
	return symbolManifestHeader + strings.Join(lines, "\n") + "\n"
}

const symbolManifestHeader = `# Top-level symbol manifest for core-ui/runtime/frag/*.js — the reference
# for TestComposedRuntimeIsSymbolComplete. One "<fragment>\t<symbol>" line
# per IIFE-body-level function/const/let/var declaration, sorted.
#
# This file is the gate's whole point: it is checked in, so DELETING a
# declaration from a fragment shows up here as a removed line that a human
# had to write. Regenerating runtime.js does not touch it.
#
# Regenerate (only when the change to the fragments is intentional):
#   GOFASTR_UPDATE_RUNTIME_SYMBOLS=1 go test ./core-ui/runtime/ -run TestComposedRuntimeIsSymbolComplete
#
`

// TestComposedRuntimeIsSymbolComplete guards against silent code loss in the
// fragments: every top-level function/const/let they declare is pinned in a
// checked-in manifest, and a declaration that disappears fails the build.
//
// What it catches: deleting (or typo-renaming) a top-level declaration in any
// frag/*.js. Regenerating runtime.js with GOFASTR_UPDATE_RUNTIME_JS=1 does not
// launder that away — the manifest is a separate artifact with a separate
// regeneration flag, so the loss stays visible as a removed line in the diff
// that a reviewer has to accept.
//
// What it does NOT catch, and never claimed to: deleting the BODY of a
// declaration while keeping its name; anything block-scoped or nested (the
// regex deliberately matches only two-space indent, the IIFE body's own
// level); object-literal method shorthand on the namespace (the attr/doc
// parity gate covers those); and any semantic regression at all. It is a
// "nothing vanished" check, not a behaviour check — the chromedp e2e suite is
// what covers behaviour.
//
// Its previous reference was `git show HEAD:core-ui/runtime/runtime.js`, which
// stopped meaning anything the moment runtime.js became a generated artifact
// that TestComposedRuntimeMatchesOnDiskFile pins byte-for-byte to
// composeFull(): the gate was comparing the composition against itself and
// could not fail.
func TestComposedRuntimeIsSymbolComplete(t *testing.T) {
	have := fragmentSymbolIndex(t)
	wantRaw, err := os.ReadFile(symbolManifestPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read %s: %v", symbolManifestPath, err)
	}
	want := string(wantRaw)
	if have == want {
		return
	}
	if os.Getenv("GOFASTR_UPDATE_RUNTIME_SYMBOLS") != "" {
		if err := os.WriteFile(symbolManifestPath, []byte(have), 0o644); err != nil {
			t.Fatalf("rewrite %s: %v", symbolManifestPath, err)
		}
		t.Logf("%s regenerated from frag/*.js — re-run without "+
			"GOFASTR_UPDATE_RUNTIME_SYMBOLS to verify.", symbolManifestPath)
		return
	}

	inHave := map[string]bool{}
	for _, l := range manifestLines(have) {
		inHave[l] = true
	}
	var gone, added []string
	for _, l := range manifestLines(want) {
		if !inHave[l] {
			gone = append(gone, l)
		}
	}
	inWant := map[string]bool{}
	for _, l := range manifestLines(want) {
		inWant[l] = true
	}
	for _, l := range manifestLines(have) {
		if !inWant[l] {
			added = append(added, l)
		}
	}
	if len(gone) > 0 {
		t.Errorf("%d top-level symbol(s) pinned in %s are GONE from frag/*.js — either "+
			"code was silently lost, or the removal is deliberate and the manifest has to say so:\n  %s",
			len(gone), symbolManifestPath, strings.Join(gone, "\n  "))
	}
	if len(added) > 0 {
		t.Errorf("%d top-level symbol(s) in frag/*.js are missing from %s:\n  %s",
			len(added), symbolManifestPath, strings.Join(added, "\n  "))
	}
	t.Logf("Regenerate with:\n  GOFASTR_UPDATE_RUNTIME_SYMBOLS=1 go test ./core-ui/runtime/ -run TestComposedRuntimeIsSymbolComplete")
}

// manifestLines strips comments and blanks so the diff below reports symbols,
// not header prose.
func manifestLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
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
