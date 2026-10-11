package ui

import (
	"strings"
	"testing"
)

// spanOf renders the token span the highlighter emits for text with class.
func spanOf(class, text string) string {
	return `<span class="` + class + `">` + text + `</span>`
}

func highlightJoined(code, lang string) string {
	lines := HighlightLines(code, lang)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func TestHighlightGoClassifiesTokens(t *testing.T) {
	src := "package main\n\ntype Pong struct {\n\tStatus string `json:\"status\"`\n}\n\n" +
		"func h(req *http.Request, fs []schema.Field) error {\n" +
		"\t// a comment with \"quotes\"\n" +
		"\tp := Pong{Status: \"ok\"}\n" +
		"\treturn render.Text(p.Status, 42)\n}\n"
	h := highlightJoined(src, "go")
	for _, want := range []string{
		spanOf("tk-kw", "package"),
		spanOf("tk-kw", "type"),
		spanOf("tk-type", "Pong"),    // after `type` and as a composite literal
		spanOf("tk-type", "string"),  // predeclared
		spanOf("tk-type", "Request"), // *pkg.Type
		spanOf("tk-type", "Field"),   // []pkg.Type
		spanOf("tk-type", "error"),
		spanOf("tk-fn", "Text"), // call site
		spanOf("tk-com", "// a comment with &quot;quotes&quot;"),
		spanOf("tk-str", "&quot;ok&quot;"),
		spanOf("tk-num", "42"),
		spanOf("tk-str", "`json:&quot;status&quot;`"),
	} {
		if !strings.Contains(h, want) {
			t.Errorf("go highlight missing %s\n%s", want, h)
		}
	}
	// A field selector is not a type: p.Status stays plain.
	if strings.Contains(h, spanOf("tk-type", "Status")) {
		t.Errorf("selector p.Status must not be classed as a type\n%s", h)
	}
}

func TestHighlightJSONAndYAMLKeys(t *testing.T) {
	j := highlightJoined(`{"name": "gofastr", "ok": true, "n": 3}`, "json")
	for _, want := range []string{
		spanOf("tk-fn", "&quot;name&quot;"),
		spanOf("tk-str", "&quot;gofastr&quot;"),
		spanOf("tk-kw", "true"),
		spanOf("tk-num", "3"),
	} {
		if !strings.Contains(j, want) {
			t.Errorf("json highlight missing %s\n%s", want, j)
		}
	}

	y := highlightJoined("app:\n  name: Meridian # the flagship\n  max-age: 30\n  tags:\n    - name: x\n  note: don't break\n", "yaml")
	for _, want := range []string{
		spanOf("tk-fn", "app"),
		spanOf("tk-fn", "name"),
		spanOf("tk-fn", "max-age"),
		spanOf("tk-com", "# the flagship"),
		spanOf("tk-num", "30"),
	} {
		if !strings.Contains(y, want) {
			t.Errorf("yaml highlight missing %s\n%s", want, y)
		}
	}
	// Values are not keys, and an apostrophe in prose opens no string.
	if strings.Contains(y, spanOf("tk-fn", "Meridian")) {
		t.Errorf("yaml value classed as a key\n%s", y)
	}
	if strings.Contains(y, `tk-str`) {
		t.Errorf("apostrophe in an unquoted yaml value opened a string\n%s", y)
	}
}

func TestHighlightShellSQLAndJS(t *testing.T) {
	sh := highlightJoined("$ go install ./cmd/gofastr # build\nmake test | tee out.log\n", "bash")
	for _, want := range []string{
		spanOf("tk-fn", "go"),
		spanOf("tk-fn", "make"),
		spanOf("tk-fn", "tee"),
		spanOf("tk-com", "# build"),
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("shell highlight missing %s\n%s", want, sh)
		}
	}
	if strings.Contains(sh, spanOf("tk-fn", "install")) {
		t.Errorf("shell argument classed as a command\n%s", sh)
	}

	sql := highlightJoined("SELECT id FROM posts WHERE title = 'a--b' -- trailing\n", "sql")
	for _, want := range []string{
		spanOf("tk-kw", "SELECT"),
		spanOf("tk-kw", "WHERE"),
		spanOf("tk-str", "&#39;a--b&#39;"),
		spanOf("tk-com", "-- trailing"),
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql highlight missing %s\n%s", want, sql)
		}
	}

	js := highlightJoined("const res = await fetch(url, { method: 'POST' });\nconst x = ok ? a : b;\n", "ts")
	for _, want := range []string{
		spanOf("tk-kw", "const"),
		spanOf("tk-kw", "await"),
		spanOf("tk-fn", "fetch"),
		spanOf("tk-fn", "method"), // object key
		spanOf("tk-str", "&#39;POST&#39;"),
	} {
		if !strings.Contains(js, want) {
			t.Errorf("js highlight missing %s\n%s", want, js)
		}
	}
	if strings.Contains(js, spanOf("tk-fn", "a")) {
		t.Errorf("ternary branch classed as an object key\n%s", js)
	}
}

func TestHighlightPlainAndEscaping(t *testing.T) {
	got := HighlightLines("<b>&\n", "text")
	if len(got) != 1 || string(got[0]) != "&lt;b&gt;&amp;" {
		t.Fatalf("plain text should be escaped and untokenized, got %q", got)
	}
	// Multi-line tokens keep their class on each line.
	lines := HighlightLines("/* one\ntwo */", "go")
	if len(lines) != 2 || !strings.Contains(string(lines[1]), spanOf("tk-com", "two */")) {
		t.Fatalf("block comment did not keep its class across lines: %q", lines)
	}
}
