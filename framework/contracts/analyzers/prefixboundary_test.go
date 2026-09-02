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
// a literal only fires when its value names part of a slash-hierarchy
// without the boundary ("/_gofastr/doc", the v0.80 uihost shape).
// Namespace literals ("screen_", "color-", "v") and constants whose value
// is bounded ("/__gofastr/runtime/") or non-hierarchical are silent; a
// dynamic prefix with an invisible value still fires.
func TestPrefixBoundaryNarrowedLiteralsAndConstants(t *testing.T) {
	ds := fixture(t, map[string]string{
		"narrow.go": `package main

import "strings"

const runtimePrefix = "/__gofastr/runtime/"
const tokenNamespace = "btk_"

func scoped(path string, mount string) bool {
	return strings.HasPrefix(path, "/_gofastr/doc") ||
		strings.HasPrefix(path, runtimePrefix) ||
		strings.HasPrefix(path, tokenNamespace) ||
		strings.HasPrefix(path, "screen_")
}

func dyn(path string, mount string) bool {
	return strings.HasPrefix(path, mount)
}
`,
	})
	assertHas(t, ds, contracts.RulePrefixSegmentBoundary)
	if got := rules(ds)[contracts.RulePrefixSegmentBoundary]; got != 2 {
		t.Errorf("expected the doc-scope literal and the dynamic mount to fire (got %d): namespace literals and bounded constants must stay silent", got)
	}
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
