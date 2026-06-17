package main

// =============================================================================
// Go source rendered as a hand-tokenized code block. The chrome (filename
// header, status dot, line count, the real copy button) and the line-number
// gutter are now owned by the framework's ui.CodeBlock; this file keeps only
// the Go-specific tokenizer.
//
// Why hand-tokenized instead of go/parser → highlighter: the design wants
// pixel-accurate control over which identifiers are styled as `tk-fn` (the
// function-call call sites) vs `tk-type` (`framework.Config`, etc.). A
// generic AST highlighter would mis-bucket those by Go's grammar. The blocks
// are short — the trade-off is fine. The token palette (.tk-*) lives in
// styles.go because it is intentionally site-specific.
//
// All token helpers escape user-supplied strings via render.Text. Literals
// passed at compile time go through the same path, no special case.
// =============================================================================

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// codeBlock renders the hand-tokenized lines through the framework's
// ui.CodeBlock — the framework owns the chrome and the line-number gutter; the
// site only supplies the Go token markup (see kw/fn_/… and ln below).
func codeBlock(filename string, lines []render.HTML) render.HTML {
	return ui.CodeBlock(ui.CodeBlockConfig{
		Filename:    filename,
		Lines:       lines,
		ShowCopy:    true,
		LineNumbers: true,
	})
}

// codeBlockScroll renders a raw source file (highlighted via the framework's
// generic tokenizer) in a framed, copyable, internally-scrolling block. Used
// for showing a long blueprint file in full — e.g. the Meridian gofastr.yml on
// /examples — without it dominating the page.
func codeBlockScroll(filename, code, lang string) render.HTML {
	return ui.CodeBlock(ui.CodeBlockConfig{
		Filename:    filename,
		Lines:       ui.HighlightLines(code, lang),
		ShowCopy:    true,
		LineNumbers: true,
		Scroll:      true,
	})
}

// ln joins a sequence of token spans into one logical source line. The
// framework wraps each line for the gutter, so a blank line still needs a
// zero-width space to keep its line box (and gutter number) from collapsing.
func ln(parts ...render.HTML) render.HTML {
	if len(parts) == 0 {
		return render.Raw("​")
	}
	return render.Join(parts...)
}

// Token helpers. One per syntax class. All produce <span class="tk-X">…</span>
// matching the v2 token palette. Plain text outside any token uses
// render.Text directly.
func kw(s string) render.HTML   { return render.Tag("span", attrClass("tk-kw"), render.Text(s)) }
func fn_(s string) render.HTML  { return render.Tag("span", attrClass("tk-fn"), render.Text(s)) }
func str_(s string) render.HTML { return render.Tag("span", attrClass("tk-str"), render.Text(s)) }
func pn(s string) render.HTML   { return render.Tag("span", attrClass("tk-pn"), render.Text(s)) }
func ty(s string) render.HTML   { return render.Tag("span", attrClass("tk-type"), render.Text(s)) }
func com(s string) render.HTML  { return render.Tag("span", attrClass("tk-com"), render.Text(s)) }

// attrClass is sugar for the one attr we set everywhere. Keeps call sites
// readable when there are 20 of them in a row.
func attrClass(c string) map[string]string { return map[string]string{"class": c} }

// itoa avoids a strconv import for the few digits we render. Three digits
// max — code blocks past 999 lines belong on a different page anyway.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
		n = -n
	}
	digits := [10]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(digits[i:])
	return b.String()
}
