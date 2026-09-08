package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1412 exists because HTML escaping was used as a scheme guard
// on URL attributes (2026-09-06/07 probes: battery/print renderShell's
// stylesheet href and auto-print script src, core-ui infinitescroll's
// noscript form action, framework/ui menu.go's MenuAction Path, and
// battery/auth's magic-link confirm page action). render.Escape proves
// a value cannot break out of the attribute; javascript: never needed
// to. Fixtures reduce each spelling and every quiet credit.

// battery/print renderShell, reduced: the Fprintf verb fills the slot.
func TestURLAttrEscapeFprintfHrefAndSrcAreReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"shell.go": `package print

import (
	"fmt"
	"strings"

	"example.com/app/core/render"
)

type shellInput struct{ AppCSSHref, AutoPrintSrc string }

func renderShell(in shellInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  <link rel=\"stylesheet\" href=\"%s\">\n", render.Escape(in.AppCSSHref))
	fmt.Fprintf(&b, "  <script src=\"%s\"></script>\n", render.Escape(in.AutoPrintSrc))
	return b.String()
}
`,
	})
	found := countRule(t, ds, contracts.RuleURLAttrEscape)
	if len(found) != 2 {
		t.Fatalf("want 2 findings (href + src), got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "urlsafe") {
		t.Errorf("message must name the scheme allow-list: %q", found[0].Message)
	}
}

// The menu Action branch (concatenation) and the stdlib
// html.EscapeString spelling, reduced.
func TestURLAttrEscapeConcatAndStdlibEscapeAreReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"menu.go": `package ui

import (
	"html"
	"strings"

	"example.com/app/core/render"
)

func writeMenuItem(b *strings.Builder, path, confirm string) {
	b.WriteString("<form method=\"post\" action=\"" + render.Escape(path) + "\">")
	b.WriteString("<button formaction=\"" + html.EscapeString(confirm) + "\">go</button>")
}
`,
	})
	found := countRule(t, ds, contracts.RuleURLAttrEscape)
	if len(found) != 2 {
		t.Fatalf("want 2 findings (render.Escape concat + html.EscapeString formaction), got %d: %v", len(found), found)
	}
}

// The three quiet credits: urlsafe.CleanAnchor before escaping (the
// menu Href branch), a scheme guard in an if-condition (uihost's
// isSafeHeadURL over urlsafe.OK), and a value rooted at a
// root-relative literal that is only appended to (embed.go's appCSS).
// A non-URL slot is escaping's right job.
func TestURLAttrEscapeCleanedGuardedAndRootedAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"quiet.go": `package ui

import (
	"fmt"
	"html"
	"strings"

	"example.com/app/core/render"
	"example.com/app/core/urlsafe"
)

func menuHref(b *strings.Builder, raw string) {
	href := urlsafe.CleanAnchor(raw)
	if href == "" {
		href = "#"
	}
	b.WriteString("<a class=\"x\" href=\"" + render.Escape(href) + "\">")
}

func directClean(b *strings.Builder, raw string) {
	b.WriteString("<a href=\"" + render.Escape(urlsafe.CleanAnchor(raw)) + "\">")
}

func headTag(href string) string {
	if !isSafeHeadURL(href) {
		return ""
	}
	return fmt.Sprintf("<link rel=\"icon\" href=\"%s\">", html.EscapeString(href))
}

func isSafeHeadURL(u string) bool { return urlsafe.OK(u, urlsafe.Resource) }

func embedHead(themeKey string) string {
	appCSS := "/__gofastr/app.css"
	if themeKey != "" {
		appCSS += "?t=" + themeKey
	}
	return fmt.Sprintf("<link rel=\"stylesheet\" href=\"%s\">", html.EscapeString(appCSS))
}

func title(b *strings.Builder, name string) {
	b.WriteString("<span class=\"print-doc\" title=\"" + render.Escape(name) + "\">")
}
`,
	})
	assertNot(t, ds, contracts.RuleURLAttrEscape,
		"cleaned, guarded, root-relative, and non-slot values are all outside the rule")
}
