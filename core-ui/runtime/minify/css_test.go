package minify

import "testing"

func TestCSS(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// --- comments ---
		{"comment", "a{color:red}/* x */b{c:d}", "a{color:red}b{c:d}"},
		{"multi-line comment", "/* a\n * b\n */\na{c:d}", "a{c:d}"},
		{"comment between tokens stays", "a{border:1px/**/solid}", "a{border:1px/**/solid}"},
		{"comment is not a combinator", ".a/**/.b{x:y}", ".a/**/.b{x:y}"},
		{"comment beside punctuation", "a/* c */{/* d */color:red/* e */;}", "a{color:red}"},
		{"comment beside a space", ".a /* c */.b{x:y}", ".a .b{x:y}"},
		{"unterminated comment", "a{c:d}/* open", "a{c:d}"},

		// --- whitespace ---
		{"declaration block", "a  {\n  color: red ;\n  margin: 0;\n}\n", "a{color:red;margin:0}"},
		{"leading and trailing", "\n\n  a{c:d}  \n", "a{c:d}"},
		{"commas", "a , b {font-family: x , y}", "a,b{font-family:x,y}"},
		{"inside parens", ":is( a , b ){c:d}", ":is(a,b){c:d}"},
		{"nested blocks", "@media (x) {\n  a { b: c; }\n}", "@media (x){a{b:c}}"},

		// --- spaces that carry meaning ---
		{"media and keeps its space", "@media screen and (min-width: 600px) {a{b:c}}", "@media screen and (min-width:600px){a{b:c}}"},
		{"descendant pseudo-class", "a :hover{c:d}", "a :hover{c:d}"},
		{"child combinator", "a > b{c:d}", "a > b{c:d}"},
		{"calc operators", "a{width: calc(100% - 2 * var(--x))}", "a{width:calc(100% - 2 * var(--x))}"},
		{"important", "a{color: red !important;}", "a{color:red !important}"},
		{"multi-value", "a{margin: 0  auto\t1px}", "a{margin:0 auto 1px}"},
		{"nesting", ".a{\n  &:hover{x:y}\n  & .b{x:y}\n}", ".a{&:hover{x:y}& .b{x:y}}"},
		{"attribute value", `[data-x="a  b"] a{c:d}`, `[data-x="a  b"] a{c:d}`},
		{"empty custom property", ":root{--x: ;}", ":root{--x: }"},
		{"semicolon run before brace", "a{b:c; ;;}", "a{b:c}"},
		{"semicolon run mid-block stays", "a{b:c;;d:e}", "a{b:c;;d:e}"},
		{"empty property then semicolons", ":root{--x: ;;}", ":root{--x: }"},

		// --- payloads kept byte for byte ---
		{"string", `a::before{content: "  /* not */  ";}`, `a::before{content:"  /* not */  "}`},
		{"escaped quote", `a{content:"a\"  b"}`, `a{content:"a\"  b"}`},
		{"single quotes", `a{content:' ; } '}`, `a{content:' ; } '}`},
		{"unquoted url", "a{background:url( data:image/svg+xml;utf8,<svg/*x*/> )}", "a{background:url(data:image/svg+xml;utf8,<svg/*x*/>)}"},
		{"url escaped paren", `a{background:url(a\)b.png)}`, `a{background:url(a\)b.png)}`},
		{"url escaped trailing space", `a{background:url(foo\ );x:y}`, `a{background:url(foo\ );x:y}`},
		{"quoted url", `a{background:url( "a b.png" )}`, `a{background:url("a b.png")}`},
		{"import", "@import url(x.css) screen;", "@import url(x.css) screen;"},
		{"hex escape terminator", `.\31 0{x:y}`, `.\31 0{x:y}`},
		{"escaped semicolon before brace", `a{content:x\;}`, `a{content:x\;}`},
		{"escaped brace keeps descendant space", `.a\{ b{x:y}`, `.a\{ b{x:y}`},
		{"non-ascii", "a{content:\"→\"}\n.é  b{c:d}", "a{content:\"→\"}.é b{c:d}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CSS(c.in)
			if got != c.want {
				t.Fatalf("CSS(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
			if again := CSS(got); again != got {
				t.Fatalf("not idempotent: %q → %q", got, again)
			}
		})
	}
}
