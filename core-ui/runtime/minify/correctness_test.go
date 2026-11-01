package minify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// correctnessCases are regressions for minifier bugs confirmed by
// executing raw vs minified code in node: a hang, dropped ASI
// newlines, fused tokens, regex/division confusion, dropped empty
// statements, missed line terminators, and a class field renamed by
// the const→let rewrite. Each input is a self-contained program whose
// completion value (or parse failure) exposes the bug, so
// TestMinifyCorrectnessNode can run raw and minified side by side.
// The `want` column pins the exact output for CI without node.
var correctnessCases = []struct {
	name, in, want string
}{
	// --- 1. presumed regex hits a newline: must fall back and advance ---
	// A second `/` on the line would close a misread regex, so these
	// pin the classification itself, not just the fallback.
	{"div after object literal", "x = {} / 2 / 1\nx", "x={}/2/1\nx"},
	{"div after keyword-named property", "var o={in:6};x = o.in / 2 / 3\nx", "var o={in:6};x=o.in/2/3\nx"},
	{"div after non-ascii ident", "var é = 6; x = é / 2 / 3\nx", "var é=6;x=é/2/3\nx"},
	{"regex fallback on newline", "var of = 4; x = of / 2\nx", "var of=4;x=of/2\nx"},
	{"regex fallback on CR", "var of = 4; x = of / 2\rx", "var of=4;x=of/2\nx"},

	// --- 2. newline after an expression-ending `}` ---
	{"arrow body newline", "const f = () => {}\ntypeof f", "let f=()=>{}\ntypeof f"},
	{"object literal newline", "var a = {x:1}\nvar b = a.x\nb", "var a={x:1}\nvar b=a.x\nb"},
	{"function expr newline", "var f = function(){ return 3 }\nf()", "var f=function(){return 3}\nf()"},
	{"class expr newline", "var C = class {}\nnew C() instanceof C", "var C=class{}\nnew C()instanceof C"},
	{"arrow body newline paren", "var g = () => {}\n(1)", "var g=()=>{}\n(1)"},
	// Statement-level `}` needs no separator; pin that the fix costs nothing there.
	{"function decl no newline", "function f(){}\nf()", "function f(){}f()"},
	{"block no newline", "if (1) {}\n2", "if(1){}2"},
	{"class decl no newline", "class A {}\nnew A() instanceof A", "class A{}new A()instanceof A"},
	{"async function decl no newline", "async function h(){}\nh() instanceof Promise", "async function h(){}h()instanceof Promise"},
	{"do-while no newline", "do {} while (0)\n3", "do{}while(0)3"},
	{"if body no newline", "if (1)\n  4", "if(1)4"},

	// --- 3. line-start characters that ASI depends on ---
	{"bang line start", "var a = 1\n!function(){ a = 2 }()\na", "var a=1\n!function(){a=2}()\na"},
	{"tilde line start", "var a = 1\n~a", "var a=1\n~a"},
	{"block line start", "var a = 1\n{ a = 2 }\na", "var a=1\n{a=2}a"},
	{"single quote line start", "var a = 1\n'abc'.length", "var a=1\n'abc'.length"},
	{"double quote line start", "var a = 1\n\"abc\".length", "var a=1\n\"abc\".length"},
	{"private field line start", "class A { a = 1\n #b = 2\n get b(){ return this.#b } }\nnew A().b", "class A{a=1\n#b=2\nget b(){return this.#b}}new A().b"},

	{"generator after class field", "class A { a\n*g(){ yield 1 } }\n[...new A().g()][0]", "class A{a\n*g(){yield 1}}[...new A().g()][0]"},
	{"leading-dot number line start", "var e = 1; e\n.5", "var e=1;e\n.5"},
	{"labelled block then regex", "foo: {}\n/ x/.test('x')", "foo:{}/ x/.test('x')"},

	// --- 4. token fusion ---
	{"regex then instanceof", "/a/ instanceof RegExp", "/a/ instanceof RegExp"},
	{"regex then in", "var o = {'/a/':1}; /a/ in o", "var o={'/a/':1};/a/ in o"},
	{"number space dot", "1 .toString()", "1 .toString()"},
	{"number newline dot", "1\n.toString()", "1 .toString()"},
	{"less-than bang", "var b = 2; 1 < !--b", "var b=2;1< !--b"},
	{"non-ascii after keyword", "var é = 5; typeof é", "var é=5;typeof é"},
	{"unicode escape after keyword", `var a = 7; typeof a`, `var a=7;typeof a`},
	{"braced unicode escape ident", `var \u{62} = 7; typeof \u{62}`, `var \u{62}=7;typeof \u{62}`},
	{"nbsp is whitespace", "var a = 1;a", "var a=1;a"},

	// --- 5. regex after the `)` of an if/while/for head ---
	{"regex after if head", "var s = 'a b', r = 0; if (s) / a/.test(s) && (r = 1)\nr", "var s='a b',r=0;if(s)/ a/.test(s)&&(r=1)\nr"},
	{"regex after while head", "var i = 0, r = 0; while (i++ < 1) / x y/.test('x y') && (r = 1)\nr", "var i=0,r=0;while(i++<1)/ x y/.test('x y')&&(r=1)\nr"},
	{"regex after for head", "var r = 0; for (var i = 0; i < 1; i++) / a b/.test('a b') && (r = 1)\nr", "var r=0;for(var i=0;i<1;i++)/ a b/.test('a b')&&(r=1)\nr"},
	{"division after grouping paren", "var a = 8, b = 2; (a) / b / 2", "var a=8,b=2;(a)/b/2"},
	{"division after keyword-named method call", "var o = {if: function(x){ return x }}; o.if(6) / 3", "var o={if:function(x){return x}};o.if(6)/3"},

	// --- 6. `;` before `}` that is an empty statement ---
	{"empty else body", "function f(x){ if (x) g(); else; }\nf(0)", "function f(x){if(x)g();else;}f(0)"},
	{"empty labelled statement", "function f(){ foo: ; }\nf()", "function f(){foo:;}f()"},

	// --- 7. CR, U+2028, U+2029 are line terminators ---
	{"return CR", "function f(){ return\r1 }\nf()", "function f(){return\n1}f()"},
	{"line comment ends at CR", "var a = 1//c\ra = 2\na", "var a=1\na=2\na"},
	{"line comment ends at LS", "var a = 1//c a = 2\na", "var a=1\na=2\na"},
	{"return LS", "function f(){ return 1 }\nf()", "function f(){return\n1}f()"},
	{"return PS", "function f(){ return 1 }\nf()", "function f(){return\n1}f()"},
	{"return CR in block comment", "function f(){ return/*\r*/1 }\nf()", "function f(){return\n1}f()"},

	// --- 8. const→let never touches a class element named const ---
	{"class field named const", "class A { const\n x = 1 }\nObject.keys(new A()).join()", "class A{const\nx=1}Object.keys(new A()).join()"},
	{"class expr field named const", "var C = class { const\n y = 2 }\nObject.keys(new C()).join()", "var C=class{const\ny=2}\nObject.keys(new C()).join()"},
	{"const in method body still rewritten", "class A { m(){ const x = 1; return x } }\nnew A().m()", "class A{m(){let x=1;return x}}new A().m()"},

	// --- template expressions keep their own context ---
	{"object literal in template expr", "var t = `${ {a:4}.a / 2 }`\nt", "var t=`${{a:4}.a/2}`\nt"},
}

func TestMinifyCorrectness(t *testing.T) {
	for _, c := range correctnessCases {
		t.Run(c.name, func(t *testing.T) {
			got := minifyWithin(t, c.in, 2*time.Second)
			if got != c.want {
				t.Errorf("\n  in:   %q\n  got:  %q\n  want: %q", c.in, got, c.want)
			}
		})
	}
}

// TestMinifyTerminates: a presumed regex that hits a newline used to
// fall back to `/` without advancing, re-reading the same byte forever
// while the output grew without bound. Each input runs under a
// deadline so a regression fails instead of hanging the suite.
func TestMinifyTerminates(t *testing.T) {
	for _, in := range []string{
		"x = {} / 2\n",
		"x = o.in / 2\n",
		"x = é / 2\n",
		"x = of / 2\n",
		"x = of / 2\r",
		"x = of / 2 ",
		"case / 2\n",
		"x = typeof / 2\n",
	} {
		minifyWithin(t, in, 2*time.Second)
	}
}

// minifyWithin runs Minify on a goroutine and fails the test (rather
// than hanging it) if it does not return within d.
func minifyWithin(t *testing.T, in string, d time.Duration) string {
	t.Helper()
	done := make(chan string, 1)
	go func() { done <- Minify(in) }()
	select {
	case out := <-done:
		return out
	case <-time.After(d):
		t.Fatalf("Minify(%q) did not return within %v", in, d)
		return ""
	}
}

// nodeHarness compiles and runs each raw/minified pair in a fresh vm
// context and reports, per pair, whether the minified program parses
// whenever the raw one does and finishes with the same completion
// value (or the same thrown error).
const nodeHarness = `
const vm = require('vm');
const fs = require('fs');
const pairs = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
function outcome(src) {
  let script;
  try { script = new vm.Script(src); } catch (e) { return 'PARSE ' + e.name; }
  try {
    const v = script.runInNewContext({}, {timeout: 1000});
    const t = typeof v;
    return 'OK ' + t + ' ' + (t === 'function' || t === 'object' ? '' : String(v));
  } catch (e) { return 'THROW ' + String(e); }
}
const res = pairs.map(p => ({raw: outcome(p[0]), min: outcome(p[1])}));
process.stdout.write(JSON.stringify(res));
`

// TestMinifyCorrectnessNode executes every correctness case raw and
// minified in node and requires identical outcomes. Skipped when node
// is not installed; TestMinifyCorrectness covers the same cases in
// pure Go.
func TestMinifyCorrectnessNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	dir := t.TempDir()
	pairs := make([][2]string, len(correctnessCases))
	for i, c := range correctnessCases {
		pairs[i] = [2]string{c.in, minifyWithin(t, c.in, 2*time.Second)}
	}
	data, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	pairsPath := filepath.Join(dir, "pairs.json")
	harness := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(pairsPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harness, []byte(nodeHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, harness, pairsPath).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var res []struct{ Raw, Min string }
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode node output: %v\n%s", err, out)
	}
	for i, c := range correctnessCases {
		r := res[i]
		if strings.HasPrefix(r.Raw, "PARSE") {
			t.Errorf("%s: raw input does not parse (%s); the case is vacuous", c.name, r.Raw)
			continue
		}
		if r.Raw != r.Min {
			t.Errorf("%s: outcome changed\n  raw: %q → %s\n  min: %q → %s", c.name, c.in, r.Raw, pairs[i][1], r.Min)
		}
	}
}
