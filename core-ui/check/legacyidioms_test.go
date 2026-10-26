package check

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLegacyIdiomsFlagsEachShape(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "old.js", `'use strict';
out.push.apply(out, root.querySelectorAll(sel));
const kids = Array.prototype.slice.call(node.children);
const hit = Array.prototype.find.apply(list, [pred]);
function wrap() { return orig.apply(this, arguments); }
Store.prototype.get = function (k) { return this.m[k]; };
Store.prototype = { get(k) { return this.m[k]; } };
function fwd() { return orig(...arguments); }
function opt() { return ready ? arguments : []; }
function alt() { return ready ? cached || arguments : []; }
function kind() { return ready ? typeof arguments : 'none'; }
`)
	res, err := LintLegacyIdioms(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, v := range res.Violations {
		lines = append(lines, v.Line)
	}
	slices.Sort(lines)
	want := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if len(lines) != len(want) {
		t.Fatalf("want violations on lines %v, got %v:\n%s", want, lines, res.Error())
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("want violations on lines %v, got %v:\n%s", want, lines, res.Error())
		}
	}
}

func TestLegacyIdiomsQuietOnModernForms(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "new.js", `'use strict';
out.push(...root.querySelectorAll(sel));
const kids = [...node.children];
const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
try { HTMLFormElement.prototype.submit.call(form); } catch (_) {}
function wrap(...a) { return orig.apply(this, a); }
send({ name: 'search', arguments: { q: 'hi' } });
const args = msg.arguments;
const maybe = msg?.arguments;
const call = {
  name: 'search',
  arguments: { q: 'hi' },
};
if (Store.prototype.get === undefined) {}
if (Store.prototype === proto) {}
const rest = (...a) => a.length;
// out.push.apply(out, list) and arguments in a comment
const s = 'Array.prototype.slice.call(x)';
class Store { get(k) { return this.m[k]; } }
`)
	res, err := LintLegacyIdioms(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("modern spellings flagged:\n%s", res.Error())
	}
}

func TestLegacyIdiomsRepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	res, err := LintLegacyIdioms(runtimeLintRoots(t, filepath.Join(repoRoot, "core-ui", "runtime"))...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("shipped JS uses a legacy idiom:\n%s", strings.TrimSpace(res.Error()))
	}
}
