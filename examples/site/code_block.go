package main

// =============================================================================
// Thin adapter over the kit's code display. ui.CodeBlock owns the chrome
// (filename header, status dot, line count, copy button), the line-number
// gutter, and the token palette: the .tk-* classes the helpers below emit
// are the same ones ui.HighlightLines emits, colored by ui.CodeBlock's
// stylesheet from the theme's Code slots (--tk-*, dark values in
// Theme.DarkCode). The site ships no CSS for any of it.
//
// The hand-tokenized helpers (kw/fn_/str_/pn/ty/com + ln) stay for blocks
// that want exact control over which identifiers read as calls vs types;
// raw source can instead go through codeBlockScroll or ui.CodeTabs, which
// highlight with ui.HighlightLines.
//
// All token helpers escape their strings via render.Text, literals
// included, so there is no special case.
// =============================================================================

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// codeBlock renders the hand-tokenized lines through the framework's
// ui.CodeBlock, the framework owns the chrome and the line-number gutter; the
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
// for showing a long blueprint file in full, e.g. the Meridian gofastr.yml on
// /examples, without it dominating the page.
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

// Token helpers. One per syntax class. Each produces <span class="tk-X">…</span>,
// the kit's token class for that role (ui.CodeBlock colors it). Plain text
// outside any token uses render.Text directly.
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
// max, code blocks past 999 lines belong on a different page anyway.
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
