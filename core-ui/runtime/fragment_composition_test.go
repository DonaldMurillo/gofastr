package runtime

import (
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fragmentSymbolDecl matches function/const/let/var declarations at every
// indentation depth. boot-embed has a nested IIFE, and rpc-stub declares its
// event-local values inside callbacks, so a fixed two-space prefix would leave
// both fragments unrepresented in the manifest.
var fragmentSymbolDecl = regexp.MustCompile(
	`(?m)^( +)(?:async\s+)?(?:function|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`,
)

// declaredSymbols returns every declaration name found in src, regardless of
// nesting. Anti-vacuity checks compare names; the manifest also records each
// declaration's indentation so nesting changes remain visible.
func declaredSymbols(src string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range fragmentSymbolDecl.FindAllStringSubmatch(src, -1) {
		out[m[2]] = struct{}{}
	}
	return out
}

var iifeBodySymbolDecl = regexp.MustCompile(
	`(?m)^  (?:async\s+)?(?:function|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`,
)

func iifeBodySymbols(src string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range iifeBodySymbolDecl.FindAllStringSubmatch(src, -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

// symbolManifestPath is the checked-in reference for the symbol-completeness
// gate below.
const symbolManifestPath = "frag/SYMBOLS.txt"

// fragmentSymbolIndex renders the manifest form of what frag/*.js declares
// right now: one "<fragment>\t<indent>\t<symbol>" line per declaration,
// sorted. Declarations at every indentation depth are included, so nested
// fragments such as boot-embed and callback-only fragments such as rpc-stub
// cannot disappear from the gate.
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
		for _, m := range fragmentSymbolDecl.FindAllStringSubmatch(string(src), -1) {
			lines = append(lines, frag+"\t"+strconv.Itoa(len(m[1]))+"\t"+m[2])
		}
	}
	sort.Strings(lines)
	return symbolManifestHeader + strings.Join(lines, "\n") + "\n"
}

func TestEveryFragmentHasSymbols(t *testing.T) {
	entries, err := fs.ReadDir(fragFS, "frag")
	if err != nil {
		t.Fatalf("read frag dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		src, err := fs.ReadFile(fragFS, "frag/"+e.Name())
		if err != nil {
			t.Fatalf("read fragment %q: %v", e.Name(), err)
		}
		if len(declaredSymbols(string(src))) == 0 {
			t.Errorf("%s contributes no symbols, so the manifest gate would pass vacuously", e.Name())
		}
	}
}

const symbolManifestHeader = `# Symbol manifest for core-ui/runtime/frag/*.js — the reference for
# TestComposedRuntimeIsSymbolComplete. One "<fragment>\t<indent>\t<symbol>"
# line per function/const/let/var declaration at any indentation depth, sorted.
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
// fragments: every function/const/let/var declaration is pinned by fragment,
// indentation depth, and name in a checked-in manifest.
//
// What it catches: deleting or typo-renaming a declaration in any frag/*.js,
// including declarations nested inside functions or callbacks. Regenerating
// runtime.js with GOFASTR_UPDATE_RUNTIME_JS=1 does not launder that away — the
// manifest is a separate artifact with a separate regeneration flag, so the
// loss stays visible as a removed line in the diff that a reviewer has to
// accept.
//
// What it does NOT catch, and never claimed to: deleting the BODY of a
// declaration while keeping its name; object-literal method shorthand on the
// namespace (the attr/doc parity gate covers those); or any semantic
// regression. It is a "nothing vanished" check, not a behaviour check — the
// chromedp e2e suite is what covers behaviour.
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

	inHave := map[string]int{}
	for _, l := range manifestLines(have) {
		inHave[l]++
	}
	var gone, added []string
	for _, l := range manifestLines(want) {
		if inHave[l] == 0 {
			gone = append(gone, l)
			continue
		}
		inHave[l]--
	}
	inWant := map[string]int{}
	for _, l := range manifestLines(want) {
		inWant[l]++
	}
	for _, l := range manifestLines(have) {
		if inWant[l] == 0 {
			added = append(added, l)
			continue
		}
		inWant[l]--
	}
	if len(gone) > 0 {
		t.Errorf("%d symbol declaration(s) pinned in %s are GONE from frag/*.js — either "+
			"code was silently lost, or the removal is deliberate and the manifest has to say so:\n  %s",
			len(gone), symbolManifestPath, strings.Join(gone, "\n  "))
	}
	if len(added) > 0 {
		t.Errorf("%d symbol declaration(s) in frag/*.js are missing from %s:\n  %s",
			len(added), symbolManifestPath, strings.Join(added, "\n  "))
	}
	if len(gone) == 0 {
		t.Logf("Regenerate with:\n  GOFASTR_UPDATE_RUNTIME_SYMBOLS=1 go test ./core-ui/runtime/ -run TestComposedRuntimeIsSymbolComplete")
	}
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
	navSymbols := iifeBodySymbols(string(navSrc))
	if len(navSymbols) == 0 {
		t.Fatal("no symbols parsed out of nav.js — the guard below would pass vacuously")
	}

	embedded, err := composeEmbed()
	if err != nil {
		t.Fatalf("composeEmbed: %v", err)
	}
	composed := iifeBodySymbols(embedded)

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
