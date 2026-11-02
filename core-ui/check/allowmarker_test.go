package check

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A trailing marker covers its own line only, and a marker counts only
// as the first thing in a comment, with a reason of its own.

const proseMarkerFixture = `(function () {
  el.title = 'Dev overlay'; // gofastr:allow(runtimeprose) dev overlay
  el.textContent = 'Upload failed';
  const doc = 'see // gofastr:allow(runtimeprose) in the docs';
  el.textContent = 'Saved your changes';
  /* gofastr:allow(runtimeprose) */ el.textContent = 'Block marker without reason';
  // note: gofastr:allow(runtimeprose) quoted mid-comment
  el.textContent = 'Quoted marker below';
  // gofastr:allow(runtimeprose) stand-alone marker with a reason
  el.textContent = 'Covered by the line above';
  // gofastr:allow(runtimeprose) --
  el.textContent = 'Punctuation is not a reason';
})();
`

func TestTrailingProseMarkerOwnLineOnly(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "p.js", proseMarkerFixture)
	res, err := LintRuntimeProse(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, res, []int{3, 5, 6, 8, 12})
}

const layerMarkerJSFixture = `(function () {
  if (k === 'data-fui-a') return; // gofastr:allow(layerprefix) refusal filter
  el.className = 'fui-card';
  /* gofastr:allow(layerprefix) */ el.className = 'fui-tile';
})();
`

func TestTrailingLayerMarkerOwnLineOnly(t *testing.T) {
	dir := writeRuntimeFixture(t, "m.js", layerMarkerJSFixture)
	res, err := LintLayerPrefixJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, res, []int{3, 4})
}

const layerMarkerGoFixture = `package fx

var a = "data-fui-a" //gofastr:allow(layerprefix) refusal filter
var b = "fui-card"
var c = "// gofastr:allow(layerprefix) a quoted marker"
var d = "fui-tile"
`

func TestTrailingLayerMarkerGoOwnLineOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fx.go"), []byte(layerMarkerGoFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintLayerPrefixGo(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, res, []int{4, 6})
}

func assertLines(t *testing.T, res *Result, want []int) {
	t.Helper()
	var got []int
	for _, v := range res.Violations {
		got = append(got, v.Line)
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings on lines %v, want %v:\n%s", got, want, res.Error())
	}
}
