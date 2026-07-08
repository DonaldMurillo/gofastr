package minify

import (
	"strings"
	"testing"
)

func TestMinify(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// --- comments ---
		{"line comment", "a=1;// foo\nb=2;", "a=1;b=2;"},
		{"block comment", "a=1;/* foo */b=2;", "a=1;b=2;"},
		{"block comment with newline", "a=1;/* foo\nbar */b=2;", "a=1;b=2;"},
		{"trailing line comment no newline", "a=1;// foo", "a=1;"},
		{"comment between idents keeps space", "let/*x*/a=1;", "let a=1;"},
		// Block comment carrying a newline still counts for ASI when
		// it follows an ASI keyword.
		{"asi keyword + comment newline", "return/* x\ny */a", "return\na"},

		// --- whitespace collapse ---
		{"spaces around operator", "a  +  b", "a+b"},
		{"tabs around operator", "a\t+\tb", "a+b"},
		{"newline around operator", "a +\n  b", "a+b"},
		{"keep one space between idents", "let  a  =  1", "let a=1"},
		{"keep space between keyword and ident", "return foo", "return foo"},

		// --- string literals ---
		{"single quotes preserved", "x='a  b';", "x='a  b';"},
		{"double quotes preserved", "x=\"a  b\";", "x=\"a  b\";"},
		{"escaped quote in string", `x='a\'b';`, `x='a\'b';`},
		{"newline in string via escape", `x='a\nb';`, `x='a\nb';`},
		{"comment-looking text inside string", "x='// not a comment';", "x='// not a comment';"},

		// --- template literals ---
		{"template basic", "x=`a  b`;", "x=`a  b`;"},
		{"template with expr", "x=`a ${b+c} d`;", "x=`a ${b+c} d`;"},
		{"template nested", "x=`a${`b${c}d`}e`;", "x=`a${`b${c}d`}e`;"},
		{"template with whitespace in expr", "x=`a${ b  +  c }d`;", "x=`a${b+c}d`;"},

		// --- regex vs division ---
		{"regex after equals", "let r=/foo/g;", "let r=/foo/g;"},
		{"regex after paren", "x(/foo/);", "x(/foo/);"},
		{"regex after return", "return /foo/;", "return/foo/;"},
		{"division between idents", "x = a / b;", "x=a/b;"},
		{"regex with slash in class", "let r=/[/]/g;", "let r=/[/]/g;"},
		{"regex with escaped slash", `let r=/\//g;`, `let r=/\//g;`},

		// --- ASI hazards ---
		{"return newline preserved", "function f(){return\n  a\n}", "function f(){return\na}"},
		{"throw newline preserved", "throw\nnew Error()", "throw\nnew Error()"},
		{"break newline preserved", "for(;;){break\n}", "for(;;){break\n}"},
		{"continue newline preserved", "for(;;){continue\n}", "for(;;){continue\n}"},

		// --- identifier fusion guard ---
		{"keyword ident", "let a=1", "let a=1"},
		{"two keywords", "const await=1", "let await=1"},
		{"number ident", "if(1 in x){}", "if(1 in x){}"},

		// --- const → let ---
		{"const decl", "const a=1;f()", "let a=1;f()"},
		// (`let[a]` / `let{a}` at statement start still parse as
		// declarations — the spec forbids an ExpressionStatement from
		// starting with `let [`.)
		{"const destructuring obj", "const {a}=x", "let{a}=x"},
		{"const destructuring arr", "const [a]=x", "let[a]=x"},
		{"const in for-of", "for(const x of xs){g(x)}", "for(let x of xs){g(x)}"},
		{"const after comment", "const/*k*/a=1", "let a=1"},
		{"const property access kept", "a.const=1", "a.const=1"},
		{"const object key kept", "x={const:1}", "x={const:1}"},
		{"const class field kept", "class A{const=1}", "class A{const=1}"},
		{"const at EOF kept", "a.b=const", "a.b=const"},

		// --- redundant ';'/',' before '}' ---
		{"semi before brace dropped", "f(){a=1;}", "f(){a=1}"},
		{"trailing comma in object dropped", "x={a:1,}", "x={a:1}"},
		// A `;` whose previous token is `)` is NEVER dropped: it may be
		// an empty statement serving as an if/while/for body, and a
		// scanner can't tell `g();}` from `if(x);}` — both end `);}`.
		// Dropping the latter (`if(x)}`) is a SyntaxError, so the byte
		// is kept in both (costs one byte on `g();}`, provably sound).
		{"empty if body semi kept", "function f(){if(x);}", "function f(){if(x);}"},
		{"empty while body semi kept", "function f(){while(g());}", "function f(){while(g());}"},
		{"empty for body semi kept", "function f(){for(;;);}", "function f(){for(;;);}"},
		{"call semi before brace kept", "f(){if(x){g();}}", "f(){if(x){g();}}"},
		{"do-while semi before brace kept", "{do{}while(x);}", "{do{}while(x);}"},
		{"trailing semi at EOF kept", "a=1;", "a=1;"},
		{"for header semis kept", "for(;;){}", "for(;;){}"},
		{"array hole commas kept", "x=[1,,]", "x=[1,,]"},
		{"empty statement else kept", "if(a);else b()", "if(a);else b()"},
		{"semi before template expr close kept-invalid", "x=`${a}`;y=1", "x=`${a}`;y=1"},

		// --- increment/decrement vs unary plus/minus ---
		// `i++` is one token; the second `+` must NOT be separated.
		{"postfix increment", "for(i=0;i<n;i++){}", "for(i=0;i<n;i++){}"},
		{"postfix decrement", "i--", "i--"},
		{"prefix increment", "++i", "++i"},
		// `a + +b` with whitespace stays separated.
		{"binary plus unary plus", "a + +b", "a+ +b"},
		// `a+++b` lexes as `a++ + b` (maximal munch) — keep verbatim.
		{"triple plus maximal munch", "a+++b", "a+++b"},
		// --- ASI after postfix ++/-- (defect: a++\nb fused to a++b) ---
		// A newline after postfix ++/-- before an identifier MUST
		// survive — otherwise the output is `a++b` (SyntaxError).
		{"postfix incr newline ident", "a++\nb", "a++\nb"},
		{"postfix decr newline ident", "a--\nb", "a--\nb"},
		// `a++\n(b)` already keeps its newline via the prefix-hazard
		// `(` path — pin it so the broader fix doesn't regress it.
		{"postfix incr newline paren", "a++\n(b)", "a++\n(b)"},

		// --- class body element separation via newline (defect) ---
		// A space does NOT trigger ASI between class elements, so a
		// newline separating two fields (or a field and a method) must
		// survive — `class A{x=1 y=2}` is a SyntaxError.
		{"class field newline field", "class A{x=1\ny=2}", "class A{x=1\ny=2}"},
		{"class static field newline", "class A{static x=1\nstatic y=2}", "class A{static x=1\nstatic y=2}"},
		{"class field newline method", "class A{x=1\nm(){}}", "class A{x=1\nm(){}}"},
		// A method body's closing `}` already terminates the element,
		// so no separator is needed before the next one.
		{"class method newline field", "class A{m(){}\nx=1}", "class A{m(){}x=1}"},
		// An explicit `;` between static fields is already valid.
		{"class static fields semi", "class A{static x=1;static y=2}", "class A{static x=1;static y=2}"},

		// --- regex with literal control bytes in character class ---
		// runtime.js has `/^[\s\x00-\x1f]+/` for URL-scheme scrubbing;
		// the NUL byte must not derail lexing.
		{"regex with NUL and control range", "x.replace(/^[\\s\x00-\x1f]+/,'')", "x.replace(/^[\\s\x00-\x1f]+/,'')"},

		// --- empty / edge ---
		{"empty", "", ""},
		{"only comment", "// just a comment", ""},
		{"only block comment", "/* x */", ""},
		{"only whitespace", "   \n  \t  ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Minify(c.in)
			if got != c.want {
				t.Errorf("\n  in:   %q\n  got:  %q\n  want: %q", c.in, got, c.want)
			}
		})
	}
}

// TestMinifyShrinks asserts the minifier never grows input. Cheap
// regression guard against any policy bug that emits more than it
// consumes.
func TestMinifyShrinks(t *testing.T) {
	samples := []string{
		"// header\nfunction foo(a, b) {\n  return a + b;\n}\n",
		"const x = {\n  a: 1,\n  b: 2,\n  c: [1, 2, 3],\n};\n",
		"if (x === y) {\n  doThing();\n} else {\n  other();\n}\n",
	}
	for i, s := range samples {
		out := Minify(s)
		if len(out) > len(s) {
			t.Errorf("sample %d grew: in=%d out=%d\nin:  %s\nout: %s", i, len(s), len(out), s, out)
		}
	}
}

// TestMinifyPreservesStringContent — string + template literal payload
// must round-trip byte-for-byte. The only thing the minifier touches
// inside ${...} is whitespace.
func TestMinifyPreservesStringContent(t *testing.T) {
	cases := []string{
		`"hello world"`,
		`'  spaces  matter  '`,
		"`raw template ${x} stuff`",
		`"// not a comment"`,
		`'/* not a block */'`,
	}
	for _, c := range cases {
		out := Minify("x=" + c + ";")
		if !strings.Contains(out, c) {
			t.Errorf("literal not preserved\n  in:  x=%s;\n  out: %s", c, out)
		}
	}
}

// TestMinifyBracesBalanced — sanity: minifier must not eat structural
// punctuation. Loose check (raw count) but catches catastrophic bugs.
func TestMinifyBracesBalanced(t *testing.T) {
	src := `function f(a, b) {
  if (a > b) { return [a, b, {x: 1, y: 2}]; }
  return /foo/.test(a);
}`
	out := Minify(src)
	for _, ch := range []string{"{", "}", "(", ")", "[", "]"} {
		if strings.Count(src, ch) != strings.Count(out, ch) {
			t.Errorf("punct %q count drift: src=%d out=%d\nout: %s", ch, strings.Count(src, ch), strings.Count(out, ch), out)
		}
	}
}
