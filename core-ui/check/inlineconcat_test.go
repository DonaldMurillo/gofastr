package check

import (
	"go/parser"
	"go/token"
	"testing"
)

// A tag built by concatenation leaves the opening half alone in its
// literal: "<script>" + js + "</script>", "<div style=\"" + css + "\">".
// Both gates read one literal at a time and used to pass that shape.

func lintScripts(t *testing.T, body string) *Result {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "x.go", "package x\n\n"+body)
	res, err := LintNoInlineScripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func lintStyles(t *testing.T, body string) *Result {
	t.Helper()
	res, err := LintNoInlineStyles(writeStyleFixture(t, body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestConcatScriptOpenTagFlagged(t *testing.T) {
	for _, body := range []string{
		"func f(js string) string { return \"<script>\" + js + \"</script>\" }\n",
		"func f(js string) string { return `<div><script type=\"module\">` + js + `</script></div>` }\n",
	} {
		if !lintScripts(t, body).HasErrors() {
			t.Errorf("concatenated inline script passed:\n%s", body)
		}
	}
}

func TestConcatExternalScriptPasses(t *testing.T) {
	body := "func f(u string) string { return \"<script src=\\\"\" + u + \"\\\"></script>\" }\n" +
		"func g(j string) string { return `<script type=\"application/json\">` + j + `</script>` }\n"
	if res := lintScripts(t, body); res.HasErrors() {
		t.Errorf("external or inert script flagged:\n%s", res.Error())
	}
}

func TestInlineImportMapFlagged(t *testing.T) {
	// CSP governs an inline import map like any inline script: under
	// default-src 'self' the browser refuses it.
	for _, body := range []string{
		"var m = `<script type=\"importmap\">{\"imports\":{}}</script>`\n",
		"import \"github.com/DonaldMurillo/gofastr/core/render\"\n\nvar _ = render.Tag(\"script\", map[string]string{\"type\": \"importmap\"}, render.HTML(\"{}\"))\n",
	} {
		if !lintScripts(t, body).HasErrors() {
			t.Errorf("inline import map passed:\n%s", body)
		}
	}
}

func TestConcatStyleAttrFlagged(t *testing.T) {
	for _, body := range []string{
		"func f(css string) string { return \"<div style=\\\"\" + css + \"\\\">\" }\n",
		"func f(css string) string { return `<p class=\"x\" style='` + css + `'>` }\n",
		"func f(css string) string { return \"<div style=\" + css + \">\" }\n",
	} {
		if !lintStyles(t, body).HasErrors() {
			t.Errorf("concatenated inline style passed:\n%s", body)
		}
	}
}

func TestStyleWordAtLiteralEndPasses(t *testing.T) {
	body := "var a = \"data-cui-style=\\\"\"\nvar b = \"el.style = \"\nvar c = \"pick a style\"\n"
	if res := lintStyles(t, body); res.HasErrors() {
		t.Errorf("non-attribute style text flagged:\n%s", res.Error())
	}
}

const cspMarkerFixture = `func f(body string) string {
	//gofastr:allow(GOFASTR1805) hashed first-paint script: the CSP carries its sha256
	return "<script>" + body + "</script>"
}

func g(body string) string { return "<script>" + body + "</script>" }

func h(body string) string {
	//gofastr:allow(GOFASTR1805)
	return "<script>" + body + "</script>"
}
`

func TestCSPMarkerWaivesOneScript(t *testing.T) {
	// GOFASTR1805 is this lint inside gofastr verify; its marker is the
	// one way to record a deliberate inline script (one the CSP allows by
	// hash) without opting the whole file out.
	res := lintScripts(t, cspMarkerFixture)
	assertLines(t, res, []int{8, 12})
}

func TestScanInScriptsLeavesMarkerToVerify(t *testing.T) {
	// The contracts pass applies its own suppressions and reports a
	// directive that matches nothing as stale, so the parsed-file entry
	// point must still report the marked site.
	src := "package x\n\n" + cspMarkerFixture
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, ScanInlineScriptsIn(fset, file, "x.go", []byte(src)), []int{5, 8, 12})
}
