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

var (
	declKeywordTail = regexp.MustCompile(`\b(?:const|let|var)\s*$`)
	listedBinding   = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*)\s*=`)
)

// topConstBindings returns the name and offset of each binding a
// top-level const declaration makes: the first, and each one after a
// comma outside brackets up to the statement's ';' (const a = 1, b = 2).
func topConstBindings(blank string, m []int) (names []string, offs []int) {
	names, offs = []string{blank[m[2]:m[3]]}, []int{m[2]}
	depth := 0
	for i := m[1]; i < len(blank) && depth >= 0; i++ {
		switch blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ';':
			if depth == 0 {
				return names, offs
			}
		case ',':
			if depth == 0 {
				if b := listedBinding.FindStringSubmatchIndex(blank[i+1:]); b != nil {
					names = append(names, blank[i+1+b[2]:i+1+b[3]])
					offs = append(offs, i+1+b[2])
				}
			}
		}
	}
	return names, offs
}

// fragmentConstWrites reports each write to a runtime fragment's
// top-level const, in any fragment, and counts the consts it read. The
// fragments share one scope once composed, so such a write throws a
// TypeError when it runs. The served runtime hides it, because the
// minifier writes every const as let; only the raw runtime (GOFASTR_DEV,
// `gofastr dev`) throws, and every e2e test runs the minified one. #598
// shipped one: kernel's `const _says`, which nav's applyDocShell assigns
// on every soft navigation, hung each one under `gofastr dev`.
//
// It matches on the Blank view, so a comment or a string ('//', 'x = 1')
// neither hides a write nor makes one. A declaration (const doc = … in
// a nested scope, or a later name in a top-level const list) is not a
// write. Nothing else counts as shadowing: a parameter or local that
// reuses a top-level const's name and is then assigned is reported, and
// the fix is to rename it. A destructuring assignment (({ x } = o)) is
// not read: telling its targets from a parameter's defaults needs a
// parser, and no fragment writes one.
func fragmentConstWrites(files []jsSource) (writes []string, consts int) {
	type binding struct {
		file string
		off  int
	}
	declared := map[binding]bool{}
	type topConst struct{ name, owner string }
	var tops []topConst
	for _, f := range files {
		for _, m := range fragmentTopConst.FindAllStringSubmatchIndex(f.Blank, -1) {
			names, offs := topConstBindings(f.Blank, m)
			for i, n := range names {
				declared[binding{f.Path, offs[i]}] = true
				tops = append(tops, topConst{n, filepath.Base(f.Path)})
			}
		}
	}
	for _, c := range tops {
		consts++
		q := regexp.QuoteMeta(c.name)
		write := regexp.MustCompile(`(?:^|[^\w$.])(` + q + `)\s*(?:(?:\*\*|<<|>>>|>>|&&|\|\||\?\?|[-+*/%&|^])?=[^=>]|\+\+|--)|(?:\+\+|--)\s*(` + q + `)\b[^.]`)
		for _, f := range files {
			for _, loc := range write.FindAllStringSubmatchIndex(f.Blank, -1) {
				at := loc[2]
				if at < 0 {
					at = loc[4]
				}
				if declared[binding{f.Path, at}] || declKeywordTail.MatchString(f.Blank[max(0, at-16):at]) {
					continue
				}
				writes = append(writes, fmt.Sprintf("%s:%d assigns %s, which %s declares const",
					filepath.Base(f.Path), f.lineOf(at), c.name, c.owner))
			}
		}
	}
	slices.Sort(writes)
	return slices.Compact(writes), consts
}

func TestFragmentConstWritesFlagged(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "a.js", `  const shared = 1;
  const routes = new Map();
  const docEl = document.documentElement;
  let free = 0;
  const first = 1, second = { k: [1, 2] }, third = f(a, b);
  const local = 1;
  local = 2;
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
  second = 2;
  third += 1;
`)
	files, err := loadJSSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, consts := fragmentConstWrites(files)
	want := []string{
		"a.js:7 assigns local, which a.js declares const",
		"b.js:1 assigns shared, which a.js declares const",
		"b.js:15 assigns second, which a.js declares const",
		"b.js:16 assigns third, which a.js declares const",
		"b.js:2 assigns routes, which a.js declares const",
		"b.js:4 assigns docEl, which a.js declares const",
		"b.js:5 assigns shared, which a.js declares const",
	}
	if !slices.Equal(got, want) {
		t.Errorf("writes:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// shared, routes, docEl, first, second, third, local; inc and s.
	if consts != 9 {
		t.Errorf("counted %d top-level consts, want 9", consts)
	}
}

//gofastr:test-reads core-ui/runtime/frag/
func TestFragmentConstsNeverReassigned(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	files, err := loadJSSources(filepath.Join(repoRoot, "core-ui", "runtime", "frag"))
	if err != nil {
		t.Fatal(err)
	}
	writes, consts := fragmentConstWrites(files)
	if consts == 0 {
		t.Fatal("found no top-level const in any fragment: the pattern or the fragment layout changed")
	}
	for _, w := range writes {
		t.Errorf("frag/%s: the raw runtime throws there (the minifier's let hides it); declare it with let", w)
	}
}
