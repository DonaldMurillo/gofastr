package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reduced from the v0.86 kernel sources the rule exists for: the toast
// builder (frag/kernel.js → headless/feedback.js), the form-errors
// module and the copy module each named a kit class.
const layerPrefixJSFixture = `(function () {
  'use strict';
  // a comment naming fui-notification is prose, not a literal
  const item = document.createElement('div');
  item.className = 'fui-toast-stack__item';
  const err = field.querySelector('.fui-field__error');
  btn.setAttribute('data-fui-open', 'x');
  const tpl = ~<span class="fui-copied">${label}</span>~;
  root.style.setProperty('--fui-surface', 'red');
  const inset = 'var(--fui-section-menu-top, 1rem)';
  // gofastr:allow(layerprefix) the refusal names the prefix it bars
  if (k.startsWith('data-fui-')) return;
  // gofastr:allow(layerprefix)
  if (k.startsWith('data-fui-x')) return;
  const hook = '[data-hui-field]';
  const cui = 'cui-visually-hidden';
})();
`

func TestLintLayerPrefixJS_FiresOnKitLiteralsOnly(t *testing.T) {
	dir := writeRuntimeFixture(t, "mod.js", strings.ReplaceAll(layerPrefixJSFixture, "~", "`"))
	res, err := LintLayerPrefixJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, v := range res.Violations {
		lines = append(lines, v.Line)
	}
	want := []int{5, 6, 7, 8, 14}
	if len(lines) != len(want) {
		t.Fatalf("findings on lines %v, want %v:\n%s", lines, want, res.Error())
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("findings on lines %v, want %v:\n%s", lines, want, res.Error())
		}
	}
	for _, v := range res.Violations {
		if !strings.Contains(v.Message, "[layer-prefix]") {
			t.Errorf("finding without the rule tag: %s", v.Message)
		}
	}
}

const layerPrefixGoFixture = `package fx

import "strings"

// A comment naming fui-button is prose.
var a = "fui-button"
var b = "--fui-button-radius"
var c = "data-fui-plugin"

// gofastr:allow(layerprefix) the refusal names the prefix it bars
var d = "data-fui-"

//gofastr:allow(layerprefix)
var e = "fui-bare-marker"

var f = ".fui-*"
var g = "[data-hui-field]"

func refuse(k string) bool {
	return strings.HasPrefix(k, "data-fui-") //gofastr:allow(layerprefix) same-line marker with a reason
}
`

func TestLintLayerPrefixGo_FiresOnKitLiteralsOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fx.go"), []byte(layerPrefixGoFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	// A test file may spell anything: it is a rig.
	if err := os.WriteFile(filepath.Join(dir, "fx_test.go"), []byte("package fx\n\nvar probe = \"fui-anything\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintLayerPrefixGo(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, v := range res.Violations {
		lines = append(lines, v.Line)
	}
	want := []int{6, 8, 14}
	if len(lines) != len(want) {
		t.Fatalf("findings on lines %v, want %v:\n%s", lines, want, res.Error())
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("findings on lines %v, want %v:\n%s", lines, want, res.Error())
		}
	}
}

func TestLintLayerPrefix_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	jsRoots, err := LayerPrefixJSRoots(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(jsRoots) < 2 {
		t.Fatalf("the JS roots hold no headless module: %v", jsRoots)
	}
	js, err := LintLayerPrefixJS(jsRoots...)
	if err != nil {
		t.Fatal(err)
	}
	if js.HasErrors() {
		t.Errorf("kernel or headless JavaScript names the kit's vocabulary:\n%s", js.Error())
	}
	gores, err := LintLayerPrefixGo(LayerPrefixGoRoots(repoRoot)...)
	if err != nil {
		t.Fatal(err)
	}
	if gores.HasErrors() {
		t.Errorf("kernel or headless Go names the kit's vocabulary:\n%s", gores.Error())
	}
}
