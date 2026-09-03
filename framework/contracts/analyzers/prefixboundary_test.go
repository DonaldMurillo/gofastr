package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1006 exists because stability.Classify matched manifest prefixes
// without a segment boundary (probe TestClassifyRequiresSegmentBoundary,
// pre-fix 7bd789e9, fixed e9e50673): {"cmd", Provisional} classified
// cmdline too. The fixtures below reduce that site to its shape, carry
// the fixed spelling as the negative, and add positives that never
// existed in this repo so the rule is proven shape-wise, not site-wise.

// The pre-fix Classify, reduced: manifest prefix r.prefix matched against
// rel with no boundary.
func TestPrefixWithoutSegmentBoundaryIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"stability.go": `package stability

import "strings"

type rule struct {
	prefix string
	tier   string
}

func Classify(importPath string, manifest []rule) (string, bool) {
	rel := strings.TrimPrefix(importPath, "github.com/DonaldMurillo/gofastr/")
	for _, r := range manifest {
		if strings.HasPrefix(rel, r.prefix) {
			return r.tier, true
		}
	}
	return "", false
}
`,
	})
	d := assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if !strings.Contains(d.Message, `HasPrefix(rel, r.prefix)`) {
		t.Errorf("message does not name the operands: %q", d.Message)
	}
	if !strings.Contains(d.Message, "cmdline") {
		t.Errorf("message does not say what a boundary-less prefix matches: %q", d.Message)
	}
}

// The fixed Classify: equality first, then HasPrefix on root+"/".
func TestPrefixSegmentBoundaryFixIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"stability.go": `package stability

import "strings"

type rule struct {
	prefix string
	tier   string
}

func Classify(importPath string, manifest []rule) (string, bool) {
	rel := strings.TrimPrefix(importPath, "github.com/DonaldMurillo/gofastr/")
	for _, r := range manifest {
		root := strings.TrimSuffix(r.prefix, "/")
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return r.tier, true
		}
	}
	return "", false
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		`rel == root || HasPrefix(rel, root+"/") is the documented fix — equality plus a boundary`)
}

// Two positives with no counterpart in this repo: different names, different
// package layout, same shape. A route matched against a mount point, and a
// document scope matched against a key.
func TestPrefixBoundaryFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"edge/tenant.go": `package tenant

import "strings"

// ownsRoute says whether routePath falls under mount. It must not claim
// /billing-archive for mount /billing.
func ownsRoute(routePath string, mount string) bool {
	return strings.HasPrefix(routePath, mount)
}
`,
		"docs/scope.go": `package docs

import (
	"net/url"
	"strings"
)

type scope struct {
	docKey string
}

func (s scope) covers(u *url.URL) bool {
	return strings.HasPrefix(u.Path, s.docKey)
}
`,
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 2 {
		t.Errorf("expected both synthetic sites to fire, got %d findings", got)
	}
}

// The narrowed literal posture (narrowed after the first whole-repo run):
// a literal or resolvable constant fires when its value names part of a
// slash-hierarchy without the boundary ("/_gofastr/doc", the v0.80 uihost
// shape) — and, narrowed a second time after the adversarial review, when
// its value contains no "/" at all but the haystack is STRONGLY path-named
// (an identifier matching path/route/url/uri/dir, or exactly rel):
// HasPrefix(importPath, "cmd") is the original stability bug with the
// manifest entry spelled as a literal, and "cmd" must not classify
// cmdline/tools. Namespace literals ("screen_", "v", "btk_") stay silent
// on haystacks that are pathish only through key or prefix naming — YAML
// keys, token names, and versions are value spaces where every longer
// sibling IS the intent. Bounded constants ("/__gofastr/runtime/") stay
// silent, and a dynamic prefix with an invisible value still fires.
func TestPrefixBoundaryNarrowedLiteralsAndConstants(t *testing.T) {
	ds := fixture(t, map[string]string{
		"narrow.go": `package main

import "strings"

const runtimePrefix = "/__gofastr/runtime/"
const tokenNamespace = "btk_"
const firstSegment = "cmd"

func namespaced(key string, prefix string) bool {
	return strings.HasPrefix(key, tokenNamespace) ||
		strings.HasPrefix(key, "screen_") ||
		strings.HasPrefix(prefix, "v")
}

func literalShape(importPath string) bool {
	return strings.HasPrefix(importPath, "cmd") ||
		strings.HasPrefix(importPath, firstSegment)
}

func literalRel(rel string) bool {
	return strings.HasPrefix(rel, "cmd")
}

func scoped(path string) bool {
	return strings.HasPrefix(path, "/_gofastr/doc")
}

func dyn(path string, mount string) bool {
	return strings.HasPrefix(path, mount)
}
`,
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 5 {
		t.Errorf("expected the slash-bearing literal, the two no-slash first-segment spellings, and the dynamic mount to fire (got %d): namespace literals on key/prefix-named haystacks and bounded constants must stay silent", got)
	}
}

// A bounded package-level constant may live in any file of the package
// (consts.go / scope.go is standard Go layout): same-package resolution,
// not same-file, is the idiom. The suggested fix docPrefix+"/" would
// match a double slash, so the silence is also the only correct advice.
func TestPrefixBoundaryResolvesSamePackageConstantsFromOtherFiles(t *testing.T) {
	ds := fixture(t, map[string]string{
		"consts.go": `package main

// docPrefix is the bounded document-script scope prefix (the uihost
// shape, fixed spelling): it carries its own trailing boundary.
const docPrefix = "/__gofastr/doc/"
`,
		"scope.go": `package main

import "strings"

// inScope: exactly the documented safe shape — the prefix resolves to
// a constant that ends in "/". The constant lives in consts.go, same
// package, sibling file.
func inScope(docPath string) bool {
	return strings.HasPrefix(docPath, docPrefix)
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"a same-package constant from a sibling file carries its own boundary — only cross-package values are dynamic")
}

// Postures pinned by the whole-repo run: the generator's screen_ file
// namespace (the haystack reads a filename through filepath.ToSlash —
// the callee names the transform, not the value) and isolation's
// "file:" DSN scheme literal (the colon terminates the scheme token) are
// legitimate idioms the strongly-path-named widening must not reach.
func TestPrefixBoundarySparesRepoIdioms(t *testing.T) {
	ds := fixture(t, map[string]string{
		"gen/gen.go": `package gen

import (
	"path/filepath"
	"strings"
)

func hasNewScreens(written []struct{ name string }) bool {
	for _, f := range written {
		if strings.HasPrefix(filepath.ToSlash(f.name), "screen_") {
			return true
		}
	}
	return false
}
`,
		"iso/iso.go": `package iso

import "strings"

func sqliteDSN(dsn string) string {
	path := dsn
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
	}
	return path
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"a transformed filename's package qualifier and a ':'-terminated scheme literal are not first-segment matches")
}

// The documented silences: a literal that carries its own boundary, the
// root itself, a concatenation at the call, and a haystack whose name
// carries no path semantics.
func TestPrefixBoundaryStaysSilentOnBoundPrefixes(t *testing.T) {
	ds := fixture(t, map[string]string{
		"bounded.go": `package main

import "strings"

func check(rel string, root string, digest string) bool {
	return rel == root ||
		strings.HasPrefix(rel, "internal/") ||
		strings.HasPrefix(rel, "/") ||
		strings.HasPrefix(rel, root+"/") ||
		strings.HasPrefix(digest, "$argon2id$")
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"every prefix here is bounded, empty-rooted, or not a path-shaped haystack")
}

// The bug is no less real under test; the rule just declines to say so
// there, because a test's fixtures are usually deliberate. The prefix is
// dynamic so the silence is attributable to the test file, not the value.
func TestPrefixBoundaryIgnoresTestFiles(t *testing.T) {
	ds := fixture(t, map[string]string{
		"classify_test.go": `package stability

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, r := range []string{"cmd", "kiln"} {
		rel := "cmdline/tools"
		if !strings.HasPrefix(rel, r) {
			t.Fatal("prefix matched")
		}
	}
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"_test.go is a documented silence")
}

// Review 5, flow-insensitive literals: a local counts as a resolvable
// literal only when it is not a parameter and EVERY assignment to it in
// the function is a string literal. The stale spelling — a parameter
// conditionally defaulted to "/api/" — lets the executed "/api" claim
// /apiary, and the mixed spelling (one literal, one dynamic assignment)
// is just as unprovable: both are dynamic and report.
func TestPrefixBoundaryEveryAssignmentMustBeALiteral(t *testing.T) {
	ds := fixture(t, map[string]string{
		"stale.go": `package main

import "strings"

func under(path, prefix string) bool {
	if prefix == "" {
		prefix = "/api/"
	}
	return strings.HasPrefix(path, prefix)
}

func reassigned(path string, base string) bool {
	local := "/api/"
	if base != "" {
		local = base
	}
	return strings.HasPrefix(path, local)
}
`,
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 2 {
		t.Errorf("expected the parameter-conditioned and mixed-assignment locals to be dynamic (got %d findings)", got)
	}
}

// Review 5, the key/prefix heuristic: key and prefix are dropped from
// the pathish-name regex entirely. API keys and token types are value
// spaces where matching every longer sibling is the intent; adding "/"
// there would break the check.
func TestPrefixBoundaryAPIKeyPrefixChecksAreNotPaths(t *testing.T) {
	ds := fixture(t, map[string]string{
		"keyed.go": `package main

import "strings"

func hasExpectedKeyPrefix(apiKey, expectedKeyPrefix string) bool {
	return strings.HasPrefix(apiKey, expectedKeyPrefix)
}

func tokenType(token, tokenPrefix string) bool {
	return strings.HasPrefix(token, tokenPrefix)
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"key- and prefix-named haystacks are value spaces, not slash trees — the rule keys on path-named haystacks only")
}

// Review 5, concatenation: a binary + chain is silent only when its
// RIGHTMOST operand is a string literal (or resolves to one) ending in
// "/", or the platform separator spelled string(os.PathSeparator) /
// string(filepath.Separator) — directly or through a local whose every
// assignment is exactly that (the whole-repo containment idiom
// absBase+string(os.PathSeparator), which a hardcoded "/" would break
// on Windows). root+"/" keeps its silence; root+leaf and a sep PARAM
// carry no proven boundary and report. A sep local conditionally
// defaulted from a caller value is the stale-default shape the literal
// path already rejects: one non-separator assignment disqualifies the
// name for good, so the re-assignment to string(filepath.Separator)
// cannot launder it back.
func TestPrefixBoundaryConcatenationNeedsRightmostBoundary(t *testing.T) {
	ds := fixture(t, map[string]string{
		"concat.go": `package main

import (
	"os"
	"path/filepath"
	"strings"
)

const slash = "/"

func unboundedConcat(path, root, leaf string) bool {
	return strings.HasPrefix(path, root+leaf)
}

func sepConcat(path, root, sep string) bool {
	return strings.HasPrefix(path, root+sep)
}

func boundedConcat(rel string, root string) bool {
	return strings.HasPrefix(rel, root+"/")
}

func constConcat(rel string, root string) bool {
	return strings.HasPrefix(rel, root+slash)
}

func osSepConcat(path, base string) bool {
	return strings.HasPrefix(path, base+string(os.PathSeparator))
}

func sepLocalConcat(rel, root string) bool {
	sep := string(filepath.Separator)
	return strings.HasPrefix(rel, root+sep)
}

func conditionalSepConcat(rel, root, cfgSep string) bool {
	sep := cfgSep
	if sep == "" {
		sep = string(filepath.Separator)
	}
	return strings.HasPrefix(rel, root+sep)
}
`,
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 3 {
		t.Errorf("expected root+leaf, the sep parameter, and the conditionally-defaulted sep to fire; the /-terminated chains and both separator spellings must stay quiet (got %d findings)", got)
	}
}

// Review 5, fixture C: a path held in neutrally named variables is out
// of reach for a parse-only pass. The rule keys on path-named haystacks
// (path/route/url/uri/dir, exactly rel, or a .URL selection); s and p
// carry no path semantics it can see, so this is a declared silence,
// not a gap it can close.
func TestPrefixBoundaryNeutrallyNamedVariablesAreOutOfReach(t *testing.T) {
	ds := fixture(t, map[string]string{
		"neutral.go": `package main

import "strings"

func under(s, p string) bool {
	return strings.HasPrefix(s, p)
}
`,
	})
	assertNot(t, ds, contracts.RulePrefixSegmentBoundary,
		"a path held in a neutrally named variable is out of reach for a parse-only pass")
}

// Same-package resolution is same-DIRECTORY resolution: the pass
// walks every directory under the root, and two directories can both
// declare `package main` (or any shared name) while being separate
// packages. Keying package constants by package NAME let one
// directory's constant overwrite or resolve for the other's
// identifiers — beta's bounded "/v1/" could silence alpha's
// unbounded "/api/users", or the reverse by file order.
func TestPrefixBoundaryPackageConstantsDoNotCrossDirectories(t *testing.T) {
	ds := fixture(t, map[string]string{
		"alpha/alpha.go": `package main

import "strings"

const base = "/api/users"

func underAlpha(routePath string) bool {
	return strings.HasPrefix(routePath, base)
}
`,
		"beta/beta.go": `package main

import "strings"

const base = "/v1/"

func underBeta(assetPath string) bool {
	return strings.HasPrefix(assetPath, base)
}
`,
	})
	d := assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if !strings.Contains(d.Message, `HasPrefix(routePath, base)`) {
		t.Errorf("expected the unbounded alpha constant to fire, got %q", d.Message)
	}
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 1 {
		t.Errorf("beta's %q is bounded by its own trailing slash; the same package NAME in another directory is a different package and must not cross-resolve (got %d findings)", "/v1/", got)
	}
}
