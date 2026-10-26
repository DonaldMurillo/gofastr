package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fragmentTopConst matches a runtime fragment's top-level const: the
// fragments compose into one IIFE's body, indented two spaces.
var fragmentTopConst = regexp.MustCompile(`(?m)^  const\s+([A-Za-z_$][\w$]*)`)

var declKeywordTail = regexp.MustCompile(`\b(?:const|let|var)\s*$`)

// crossFragmentConstWrites reports each write, in one fragment, to a
// top-level const another fragment declares, and counts the consts it
// read. The fragments share one scope once composed, so such a write
// throws a TypeError when it runs. The served runtime hides it, because
// the minifier writes every const as let; only the raw runtime
// (GOFASTR_DEV, `gofastr dev`) throws, and every e2e test runs the
// minified one. #598 shipped one: kernel's `const _says`, which nav's
// applyDocShell assigns on every soft navigation, hung each one under
// `gofastr dev`.
//
// It matches on the Blank view, so a comment or a string ('//', 'x = 1')
// neither hides a write nor makes one. A declaration (const doc = … in
// a nested scope) is not a write. Nothing else counts as shadowing: a
// parameter or local that reuses a shared top-level name and is then
// assigned is reported, and the fix is to rename it.
func crossFragmentConstWrites(files []jsSource) (writes []string, consts int) {
	for _, owner := range files {
		for _, m := range fragmentTopConst.FindAllStringSubmatch(owner.Blank, -1) {
			consts++
			q := regexp.QuoteMeta(m[1])
			write := regexp.MustCompile(`(?:^|[^\w$.])(` + q + `)\s*(?:(?:\*\*|<<|>>>|>>|&&|\|\||\?\?|[-+*/%&|^])?=[^=>]|\+\+|--)|(?:\+\+|--)\s*(` + q + `)\b[^.]`)
			for _, f := range files {
				if f.Path == owner.Path {
					continue
				}
				for _, loc := range write.FindAllStringSubmatchIndex(f.Blank, -1) {
					at := loc[2]
					if at < 0 {
						at = loc[4]
					}
					if declKeywordTail.MatchString(f.Blank[max(0, at-16):at]) {
						continue
					}
					writes = append(writes, fmt.Sprintf("%s:%d assigns %s, which %s declares const",
						filepath.Base(f.Path), f.lineOf(at), m[1], filepath.Base(owner.Path)))
				}
			}
		}
	}
	slices.Sort(writes)
	return writes, consts
}

func TestFragmentConstWritesFlagged(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "a.js", `  const shared = 1;
  const routes = new Map();
  const docEl = document.documentElement;
  let free = 0;
`)
	writeJS(t, dir, "b.js", `  shared = 2;
  if (href.startsWith('//')) routes = new Map();
  G.scan(docEl);
  docEl = null;
  ++shared;
  free = 1;
  obj.shared = 3;
  if (shared === 1) {}
  const inc = (shared) => shared + 1;
  // shared = 4 in a comment
  const s = 'routes = x';
  function f() {
    const routes = [];
  }
`)
	files, err := loadJSSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, consts := crossFragmentConstWrites(files)
	want := []string{
		"b.js:1 assigns shared, which a.js declares const",
		"b.js:2 assigns routes, which a.js declares const",
		"b.js:4 assigns docEl, which a.js declares const",
		"b.js:5 assigns shared, which a.js declares const",
	}
	if !slices.Equal(got, want) {
		t.Errorf("writes:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	if consts != 5 {
		t.Errorf("counted %d top-level consts, want 5", consts)
	}
}

//gofastr:test-reads core-ui/runtime/frag/
func TestFragmentConstsNotAssignedElsewhere(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	files, err := loadJSSources(filepath.Join(repoRoot, "core-ui", "runtime", "frag"))
	if err != nil {
		t.Fatal(err)
	}
	writes, consts := crossFragmentConstWrites(files)
	if consts == 0 {
		t.Fatal("found no top-level const in any fragment: the pattern or the fragment layout changed")
	}
	for _, w := range writes {
		t.Errorf("frag/%s: the raw runtime throws there (the minifier's let hides it); declare it with let", w)
	}
}
