package main

// =============================================================================
// Go source rendered as a hand-tokenized <pre>. Mirrors the prototype's
// .code / .code__head / .code__body / .ln structure verbatim so the CSS in
// styles.go works without modification.
//
// Why hand-tokenized instead of go/parser → highlighter: the design wants
// pixel-accurate control over which identifiers are styled as `tk-fn` (the
// function-call call sites) vs `tk-type` (`framework.Config`, etc.). A
// generic AST highlighter would mis-bucket those by Go's grammar. The pre is
// short — the trade-off is fine for a hero block. If we add more code on
// other pages, lift this into framework/ui/CodeBlock with a proper
// highlight pass; mark this as the porting target then.
//
// All token helpers escape user-supplied strings via render.Escape. Literals
// passed at compile time are also escaped — same code path, no special case.
// =============================================================================

import (
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// codeBlockSeq hands each codeBlock a process-unique id so the copy button
// can target its own <pre> via #id. The exact value is irrelevant — only
// that the button and its pre share it within one render (they do, minted
// together below), so it stays correct across SSR and client-nav renders.
var codeBlockSeq atomic.Uint64

// codeBlock renders the chrome (filename + line-count + copy button) plus
// the body lines. The line count is len(lines) — every entry, including
// blank ones, renders as a visible line (see ln), so the count matches
// what's on screen.
func codeBlock(filename string, lines []render.HTML) render.HTML {
	id := "codeblk-" + itoa(int(codeBlockSeq.Add(1)))
	return render.Tag("div", attrClass("code"),
		render.Tag("div", attrClass("code__head"),
			// Status dot — green, matches the file's "alive" pill in the
			// prototype. Class lives in styles.go (CSP forbids inline style).
			render.Tag("span", attrClass("alive"), render.Raw("")),
			render.Tag("span", attrClass("file"), render.Text(filename)),
			render.Tag("span", attrClass("right"),
				render.Tag("span", nil, render.Text(itoa(len(lines))+" lines")),
				// Real copy button: the runtime's data-fui-copy-text-from
				// handler copies the <pre>'s textContent, toggles .fui-copied
				// for ~1.2s (drives the label swap below), and announces
				// "Copied" to screen readers via data-fui-copy-announce.
				render.Tag("button", map[string]string{
					"class":                   "copy",
					"type":                    "button",
					"data-fui-copy-text-from": "#" + id,
					"data-fui-copy-announce":  "Copied",
					"aria-label":              "Copy code to clipboard",
				},
					render.Tag("span", attrClass("copy__label"), render.Text("copy")),
					render.Tag("span", attrClass("copy__done"), render.Text("copied")),
				),
			),
		),
		// IMPORTANT: no whitespace between <pre> and the first <span class="ln">.
		// `white-space: pre` on .code__body would otherwise render that whitespace
		// as a stray leading newline. We spread the line slice into Tag's
		// children which serializes each child back-to-back, no separator.
		render.Tag("pre", map[string]string{"class": "code__body", "id": id}, lines...),
	)
}

// ln wraps a sequence of token spans as one logical source line. A blank
// line still emits a line box (via a zero-width space) so its gutter number
// shows — without it, the empty <span class="ln"> collapses to zero height
// and the numbers look like they skip (1, 3, 4…).
func ln(parts ...render.HTML) render.HTML {
	if len(parts) == 0 {
		return render.Tag("span", attrClass("ln"), render.Raw("\u200b"))
	}
	return render.Tag("span", attrClass("ln"), parts...)
}

// Token helpers. One per syntax class. All produce <span class="tk-X">…</span>
// matching the v2.css color scale. Plain text outside any token uses
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
