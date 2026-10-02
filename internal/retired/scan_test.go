package retired

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// scanAll renders every visited tag as "tag[attr1 attr2][class tokens]"
// with attrs lowercased the way the matcher sees them.
func scanAll(html string) []string {
	var out []string
	Scan([]byte(html), func(tag []byte, attrs [][]byte, classes [][]byte) {
		names := make([]string, len(attrs))
		for i, a := range attrs {
			names[i] = strings.ToLower(string(a))
		}
		toks := make([]string, len(classes))
		for i, c := range classes {
			toks[i] = string(c)
		}
		out = append(out, fmt.Sprintf("%s%v%v", tag, names, toks))
	})
	return out
}

func TestScanStartTags(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []string
	}{
		{"plain", `<div class="ui-button">x</div>`, []string{`div[class][ui-button]`}},
		{"unquoted value", `<div class=ui-button data-x=1>`, []string{`div[class data-x][ui-button]`}},
		{"single quoted", "<div class='ui-button' data-y='a b'>", []string{`div[class data-y][ui-button]`}},
		{"attr without value", `<input disabled class=chk>`, []string{`input[disabled class][chk]`}},
		{"uppercase names", `<DIV CLASS="Up">`, []string{`DIV[class][Up]`}},
		{"class repeated", `<p class="a" class="b">`, []string{`p[class class][a b]`}},
		{"entities in values", `<a title="&quot;q&quot;" class="a&amp;b">`, []string{`a[title class][a&amp;b]`}},
		{"gt inside quoted value", `<div class="a>b" data-x="1">`, []string{`div[class data-x][a>b]`}},
		{"lt in text", `3 < 5 and a < b <div class=x>`, []string{`div[class][x]`}},
		{"comment skipped", `<!-- <div class="ui-button"> --><span>`, []string{`span[][]`}},
		{"comment with an early >", `<!-- a > b <div class="ui-button"> --><span>`, []string{`span[][]`}},
		{"doctype skipped", `<!DOCTYPE html><html>`, []string{`html[][]`}},
		{"end tag skipped", `<div class=x></div><b>`, []string{`div[class][x]`, `b[][]`}},
		{"self closing", `<br/><img src="a.png" />`, []string{`br[][]`, `img[src][]`}},
		{"slash in unquoted value", `<a href=/a/b>x</a>`, []string{`a[href][]`}},
		{"multiple class tokens", "<td class=\"a  b\tc\nd\">", []string{`td[class][a b c d]`}},
		{"script body skipped", `<script>var s = '<div class="ui-button">'; if (a<b) {}</script><p class=after>`,
			[]string{`script[][]`, `p[class][after]`}},
		{"style body skipped", `<style>.ui-button { color: red }</style><em>`, []string{`style[][]`, `em[][]`}},
		{"textarea body skipped", `<textarea><div class="ui-button"></textarea>`, []string{`textarea[][]`}},
		{"title body skipped", `<title>a < b "c"</title>`, []string{`title[][]`}},
		{"uppercase raw text tag", `<STYLE>.x{}</STYLE><i>`, []string{`STYLE[][]`, `i[][]`}},
		{"attr with quoted markup value", `<div data-a="<b class=x>">`, []string{`div[data-a][]`}},
		{"equals before name", `<div =x class=y>`, []string{`div[=x class][y]`}},
		{"empty class", `<div class="">`, []string{`div[class][]`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanAll(tc.html)
			if !equalStrings(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Unterminated input must not panic or hang: the scanner yields what it
// managed to parse and stops.
func TestScanUnterminated(t *testing.T) {
	for _, html := range []string{
		`<div class="x`,
		`<div class=`,
		`<div `,
		`<div`,
		`<`,
		`<!-- comment never ends`,
		`<!DOCTYPE html unparsed`,
		`</div`,
		`<script>never ends`,
		`<style>.a{`,
		`<div data-x='unterminated`,
		`<div =`,
		`<div /`,
	} {
		done := make(chan struct{})
		go func(src string) { _ = scanAll(src); close(done) }(html)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("scan of %q did not terminate", html)
		}
	}
}

func FuzzScan(f *testing.F) {
	for _, s := range []string{
		`<div class="ui-button--lg" data-fui-signal>`,
		`<script>a<b</script><p class=x>`,
		`<!-- c --><!DOCTYPE html><div class='a>b'>`,
		`<div =x / <span class=`,
		`<style>.a{}</style><textarea><b></textarea>`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, html string) {
		tags := 0
		lts := 0
		for i := range len(html) {
			if html[i] == '<' {
				lts++
			}
		}
		Scan([]byte(html), func(tag []byte, attrs [][]byte, classes [][]byte) {
			tags++
			for _, a := range attrs {
				if len(a) == 0 {
					t.Fatalf("empty attribute name reported for %q", html)
				}
			}
		})
		if tags > lts {
			t.Fatalf("%d tags from %d '<' bytes in %q", tags, lts, html)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
