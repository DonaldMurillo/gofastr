package attrscan

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The regexps the two SSR scans used before the byte scan. They are the
// oracle: Values must yield exactly their FindAllStringSubmatch captures.
var (
	refOracle    = regexp.MustCompile(`[\s/]data-cui-(?:signal-set|signal-inc|signal-toggle|signal|computed)="([^":]+)`)
	markerOracle = regexp.MustCompile(`[\s/]data-cui-(?:comp|scope)="([a-zA-Z0-9_:.-]+)"`)
)

var (
	refNames    = []string{"signal-set", "signal-inc", "signal-toggle", "signal", "computed"}
	markerNames = []string{"comp", "scope"}
)

func refByte(c byte) bool { return c != '"' && c != ':' }

func markerByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return c == '_' || c == ':' || c == '.' || c == '-'
}

func oracle(re *regexp.Regexp, html string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

func checkBoth(t *testing.T, html string) {
	t.Helper()
	if got, want := slices.Collect(Values(html, refNames, refByte, false)), oracle(refOracle, html); !slices.Equal(got, want) {
		t.Errorf("signal refs in %q: got %q, want %q", html, got, want)
	}
	if got, want := slices.Collect(Values(html, markerNames, markerByte, true)), oracle(markerOracle, html); !slices.Equal(got, want) {
		t.Errorf("sheet markers in %q: got %q, want %q", html, got, want)
	}
}

var parityCases = []string{
	``,
	`data-cui-signal="a"`,
	` data-cui-signal="a"`,
	`<span data-cui-signal="a">x</span>`,
	`<b data-cui-signal-set="b:1" data-cui-signal-inc="c:2">`,
	`<i data-cui-signal-toggle="d" data-cui-computed="e">`,
	`<pre>data-cui-signal="ghost"</pre>`,
	`<x/data-cui-signal="slash">`,
	"<x\tdata-cui-signal=\"tab\"\ndata-cui-signal=\"nl\"\fdata-cui-comp=\"ff\"\rdata-cui-scope=\"cr\">",
	"<x\vdata-cui-signal=\"vt\">",
	`<x data-cui-signal="">`,
	`<x data-cui-signal=":v">`,
	`<x data-cui-signal="open`,
	`<x data-cui-signal='single'>`,
	`<x data-cui-signal ="spaced">`,
	`<x data-cui-signals="plural">`,
	`<x data-cui-signal="a data-cui-signal="b">`,
	`<x data-cui-signal="a data-cui-signal=" data-cui-signal="c">`,
	`<x data-cui-comp="ui-button">`,
	`<x data-cui-comp="ui-button" data-cui-scope="site.header">`,
	`<x data-cui-comp="bad name">`,
	`<x data-cui-comp="unterminated`,
	`<x data-cui-comp="">`,
	`<x data-cui-comp="a"data-cui-comp="b">`,
	`<x data-cui-comp="a" data-cui-comp="a">`,
	`<x data-cui-data-cui-comp="a">`,
	` data-cui- data-cui-comp="z"`,
	"<x data-cui-signal=\"caf\xc3\xa9\" data-cui-signal=\"\xff\xfe\">",
	`  data-cui-signal="a"data-cui-signal="b" /data-cui-comp="c"`,
}

func TestValuesMatchOracleCases(t *testing.T) {
	for _, c := range parityCases {
		checkBoth(t, c)
	}
}

func FuzzValuesMatchOracle(f *testing.F) {
	for _, c := range parityCases {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, html string) {
		checkBoth(t, html)
	})
}

func TestValuesStopsWhenYieldRefuses(t *testing.T) {
	html := ` data-cui-comp="a" data-cui-comp="b" data-cui-comp="c"`
	var got []string
	for v := range Values(html, markerNames, markerByte, true) {
		got = append(got, v)
		if v == "b" {
			break
		}
	}
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("got %q, want [a b]", got)
	}
}

// page is a document the size of the examples/meridian homepage (74 KB)
// with a handful of markers, the shape every SSR response carries.
var page = strings.Repeat(`<div class="fui-card"><p>Plain paragraph text with some <a href="/x">links</a> and words.</p></div>`+"\n", 740) +
	` <i data-cui-comp="ui-button" data-cui-signal="cart.count">`

func BenchmarkValuesPage(b *testing.B) {
	b.SetBytes(int64(len(page)))
	for b.Loop() {
		for range Values(page, refNames, refByte, false) {
		}
	}
}

func BenchmarkOraclePage(b *testing.B) {
	b.SetBytes(int64(len(page)))
	for b.Loop() {
		refOracle.FindAllStringSubmatch(page, -1)
	}
}

// A value class that accepts spaces but refuses 'd' lets an unclosed
// value end right where the next attribute begins, with a space as its
// last byte. The regexp resumes after the match, so that space cannot be
// the next match's boundary.
func TestValuesResumeAfterMatch(t *testing.T) {
	re := regexp.MustCompile(`[\s/]data-cui-(?:a)="([^"d]+)`)
	noD := func(c byte) bool { return c != '"' && c != 'd' }
	for _, html := range []string{
		` data-cui-a="x data-cui-a="y`,
		` data-cui-a="x  data-cui-a="y`,
		` data-cui-a="data-cui-a="y`,
	} {
		got := slices.Collect(Values(html, []string{"a"}, noD, false))
		if want := oracle(re, html); !slices.Equal(got, want) {
			t.Errorf("%q: got %q, want %q", html, got, want)
		}
	}
}
