package ownstyle

import (
	"fmt"
	"testing"
)

// tok is a compact expected-token: type, text, line, col.
type tok struct {
	typ       TokenType
	text      string
	line, col int
}

func checkTokens(t *testing.T, src string, want []tok) {
	t.Helper()
	got := Tokenize(src)
	if len(got) != len(want) {
		t.Fatalf("tokenize %q: got %d tokens, want %d:\n%s",
			src, len(got), len(want), dumpTokens(got))
	}
	for i, w := range want {
		g := got[i]
		if g.Type != w.typ || g.Text != w.text || g.Line != w.line || g.Col != w.col {
			t.Errorf("tokenize %q [%d]: got %s %q %d:%d, want %s %q %d:%d",
				src, i, g.Type, g.Text, g.Line, g.Col, w.typ, w.text, w.line, w.col)
		}
	}
}

func dumpTokens(toks []Token) string {
	out := ""
	for i, tk := range toks {
		out += fmt.Sprintf("  [%d] %s %q %d:%d\n", i, tk.Type, tk.Text, tk.Line, tk.Col)
	}
	return out
}

func TestTokenizeIdentsAndFunctions(t *testing.T) {
	checkTokens(t, ".column.over-limit .count{}", []tok{
		{TokenDelim, ".", 1, 1},
		{TokenIdent, "column", 1, 2},
		{TokenDelim, ".", 1, 8},
		{TokenIdent, "over-limit", 1, 9},
		{TokenWhitespace, " ", 1, 19},
		{TokenDelim, ".", 1, 20},
		{TokenIdent, "count", 1, 21},
		{TokenOpenCurly, "{", 1, 26},
		{TokenCloseCurly, "}", 1, 27},
	})
	// A function token is an ident plus '('.
	checkTokens(t, "var(--spacing-md)", []tok{
		{TokenFunction, "var(", 1, 1},
		{TokenIdent, "--spacing-md", 1, 5},
		{TokenCloseParen, ")", 1, 17},
	})
}

func TestTokenizeCustomPropertyIdents(t *testing.T) {
	// -- names: '--' then name chars; the classic custom property and
	// the custom media name both tokenize as one ident.
	checkTokens(t, "--above-md --dark --", []tok{
		{TokenIdent, "--above-md", 1, 1},
		{TokenWhitespace, " ", 1, 11},
		{TokenIdent, "--dark", 1, 12},
		{TokenWhitespace, " ", 1, 18},
		{TokenIdent, "--", 1, 19},
	})
	// A single '-' with nothing ident-ish after is a delim.
	checkTokens(t, "a - b", []tok{
		{TokenIdent, "a", 1, 1},
		{TokenWhitespace, " ", 1, 2},
		{TokenDelim, "-", 1, 3},
		{TokenWhitespace, " ", 1, 4},
		{TokenIdent, "b", 1, 5},
	})
}

func TestTokenizeNumbers(t *testing.T) {
	checkTokens(t, "1e3px 0.5rem .4 -2px 85%", []tok{
		{TokenDimension, "1e3px", 1, 1},
		{TokenWhitespace, " ", 1, 6},
		{TokenDimension, "0.5rem", 1, 7},
		{TokenWhitespace, " ", 1, 13},
		{TokenNumber, ".4", 1, 14},
		{TokenWhitespace, " ", 1, 16},
		{TokenDimension, "-2px", 1, 17},
		{TokenWhitespace, " ", 1, 21},
		{TokenPercentage, "85%", 1, 22},
	})
	// '1e' with no exponent digit is a dimension named 'e'.
	checkTokens(t, "1e", []tok{{TokenDimension, "1e", 1, 1}})
	// '+' alone is a delim; '+2' is a number.
	checkTokens(t, "a + 2", []tok{
		{TokenIdent, "a", 1, 1},
		{TokenWhitespace, " ", 1, 2},
		{TokenDelim, "+", 1, 3},
		{TokenWhitespace, " ", 1, 4},
		{TokenNumber, "2", 1, 5},
	})
	checkTokens(t, "+2px", []tok{{TokenDimension, "+2px", 1, 1}})
}

func TestTokenizeURL(t *testing.T) {
	checkTokens(t, "url(img.png)", []tok{{TokenURL, "url(img.png)", 1, 1}})
	// Case-insensitive keyword, whitespace around the value.
	checkTokens(t, "URL( a.png )", []tok{{TokenURL, "URL( a.png )", 1, 1}})
	// A space inside the value is a bad-url that still swallows to ')'.
	checkTokens(t, "url(a b)", []tok{{TokenBadURL, "url(a b)", 1, 1}})
	// Quoted url is a single url token (CSS Syntax 3 §4.3.6).
	checkTokens(t, `url("a.png")`, []tok{{TokenURL, `url("a.png")`, 1, 1}})
	// Escaped paren inside an unquoted url stays in the token.
	checkTokens(t, `url(a\).png)`, []tok{{TokenURL, `url(a\).png)`, 1, 1}})
	// 'other(' does not trigger the url path.
	checkTokens(t, "other(a b)", []tok{
		{TokenFunction, "other(", 1, 1},
		{TokenIdent, "a", 1, 7},
		{TokenWhitespace, " ", 1, 8},
		{TokenIdent, "b", 1, 9},
		{TokenCloseParen, ")", 1, 10},
	})
}

func TestTokenizeStrings(t *testing.T) {
	checkTokens(t, `"a\"b" 'it''s'`, []tok{
		{TokenString, `"a\"b"`, 1, 1},
		{TokenWhitespace, " ", 1, 7},
		{TokenString, `'it'`, 1, 8},
		{TokenString, `'s'`, 1, 12},
	})
	// Unescaped newline: bad-string stops before the newline, and the
	// newline itself becomes whitespace on the next line.
	checkTokens(t, "\"a\nb\"", []tok{
		{TokenBadString, `"a`, 1, 1},
		{TokenWhitespace, "\n", 1, 3},
		{TokenIdent, "b", 2, 1},
		{TokenString, `"`, 2, 2},
	})
}

func TestTokenizeEscapesInIdents(t *testing.T) {
	// Escapes are preserved verbatim in the token text.
	checkTokens(t, `.\75 rl(x)`, []tok{
		{TokenDelim, ".", 1, 1},
		{TokenFunction, `\75 rl(`, 1, 2}, // not the url token: text differs
		{TokenIdent, "x", 1, 9},
		{TokenCloseParen, ")", 1, 10},
	})
	checkTokens(t, `.col\umn`, []tok{
		{TokenDelim, ".", 1, 1},
		{TokenIdent, `col\umn`, 1, 2},
	})
}

func TestTokenizePositionsAcrossLines(t *testing.T) {
	src := ".a {\n  padding: 16px;\n  color: #52525B;\n}\n"
	got := Tokenize(src)
	want := []tok{
		{TokenDelim, ".", 1, 1},
		{TokenIdent, "a", 1, 2},
		{TokenWhitespace, " ", 1, 3},
		{TokenOpenCurly, "{", 1, 4},
		{TokenWhitespace, "\n  ", 1, 5},
		{TokenIdent, "padding", 2, 3},
		{TokenColon, ":", 2, 10},
		{TokenWhitespace, " ", 2, 11},
		{TokenDimension, "16px", 2, 12},
		{TokenSemicolon, ";", 2, 16},
		{TokenWhitespace, "\n  ", 2, 17},
		{TokenIdent, "color", 3, 3},
		{TokenColon, ":", 3, 8},
		{TokenWhitespace, " ", 3, 9},
		{TokenHash, "#52525B", 3, 10},
		{TokenSemicolon, ";", 3, 17},
		{TokenWhitespace, "\n", 3, 18},
		{TokenCloseCurly, "}", 4, 1},
		{TokenWhitespace, "\n", 4, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens, want %d:\n%s", len(got), len(want), dumpTokens(got))
	}
	for i, w := range want {
		g := got[i]
		if g.Type != w.typ || g.Text != w.text || g.Line != w.line || g.Col != w.col {
			t.Errorf("[%d]: got %s %q %d:%d, want %s %q %d:%d",
				i, g.Type, g.Text, g.Line, g.Col, w.typ, w.text, w.line, w.col)
		}
	}
}
func TestTokenizeCDOCDCAndComments(t *testing.T) {
	checkTokens(t, "<!-- -->", []tok{
		{TokenCDO, "<!--", 1, 1},
		{TokenWhitespace, " ", 1, 5},
		{TokenCDC, "-->", 1, 6},
	})
	// Comments are tokens so doc comments survive parsing.
	checkTokens(t, "/* hi */ .a", []tok{
		{TokenComment, "/* hi */", 1, 1},
		{TokenWhitespace, " ", 1, 9},
		{TokenDelim, ".", 1, 10},
		{TokenIdent, "a", 1, 11},
	})
	// A '>' on its own is a delim.
	checkTokens(t, "a > b", []tok{
		{TokenIdent, "a", 1, 1},
		{TokenWhitespace, " ", 1, 2},
		{TokenDelim, ">", 1, 3},
		{TokenWhitespace, " ", 1, 4},
		{TokenIdent, "b", 1, 5},
	})
}

func TestTokenizeAtKeywordAndHash(t *testing.T) {
	checkTokens(t, "@media (min-width: 900px)", []tok{
		{TokenAtKeyword, "@media", 1, 1},
		{TokenWhitespace, " ", 1, 7},
		{TokenOpenParen, "(", 1, 8},
		{TokenIdent, "min-width", 1, 9},
		{TokenColon, ":", 1, 18},
		{TokenWhitespace, " ", 1, 19},
		{TokenDimension, "900px", 1, 20},
		{TokenCloseParen, ")", 1, 25},
	})
	// '#' not followed by a name char is a delim (id selector start of
	// nothing, or a colour shorthand typo).
	checkTokens(t, "#fff # x", []tok{
		{TokenHash, "#fff", 1, 1},
		{TokenWhitespace, " ", 1, 5},
		{TokenDelim, "#", 1, 6},
		{TokenWhitespace, " ", 1, 7},
		{TokenIdent, "x", 1, 8},
	})
}

func TestTokenizeUnterminatedCommentAndStringEOF(t *testing.T) {
	// Unterminated comment: one comment token to EOF.
	checkTokens(t, ".a /* trailing", []tok{
		{TokenDelim, ".", 1, 1},
		{TokenIdent, "a", 1, 2},
		{TokenWhitespace, " ", 1, 3},
		{TokenComment, "/* trailing", 1, 4},
	})
	// Unterminated string at EOF is still a string token.
	checkTokens(t, `"abc`, []tok{{TokenString, `"abc`, 1, 1}})
	// Unterminated url at EOF is bad-url.
	checkTokens(t, "url(a", []tok{{TokenBadURL, "url(a", 1, 1}})
}

func TestTokenizeCRPairsCountOneLine(t *testing.T) {
	// \r\n is one line break; column counting restarts.
	checkTokens(t, "a\r\nb", []tok{
		{TokenIdent, "a", 1, 1},
		{TokenWhitespace, "\r\n", 1, 2},
		{TokenIdent, "b", 2, 1},
	})
}
