package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Every link a rendered doc page carries reaches a page: a sibling doc
// is its /docs/<slug> page in the catalog, and nothing relative is left
// for the browser to resolve against /docs/ (2026-10-27 bug bash W4: the
// corpus's a2a.md -> entity-declarations.md link landed on the raw
// markdown route live and on a 404 in the static export).
func TestDocPagesLinkSiblingPages(t *testing.T) {
	href := regexp.MustCompile(`href="([^"]*)"`)
	rewrote := 0
	for _, e := range flatDocs() {
		out := string((&DocPageScreen{Entry: e}).Render())
		for _, m := range href.FindAllStringSubmatch(out, -1) {
			h := m[1]
			switch {
			case strings.HasPrefix(h, "/docs/"):
				slug, _, _ := strings.Cut(strings.TrimPrefix(h, "/docs/"), "#")
				// "" is the docs index.
				if _, _, ok := findDocEntry(slug); slug != "" && !ok {
					t.Errorf("%s links %s, which is no catalog page", e.Slug, h)
				}
				rewrote++
			case strings.HasPrefix(h, repoBlobURL):
				rewrote++
			case strings.HasPrefix(h, "/"), strings.HasPrefix(h, "#"), strings.Contains(h, ":"):
			default:
				t.Errorf("%s keeps the relative link %q", e.Slug, h)
			}
		}
	}
	if rewrote == 0 {
		t.Fatal("no doc page carried a sibling or repo link; the corpus scan read nothing")
	}
}

func TestSiteDocHref(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"entity-declarations.md", "/docs/entity-declarations"},
		{"a2a.md#tasks", "/docs/a2a#tasks"},
		{"../../../ROADMAP.md", repoBlobURL + "ROADMAP.md"},
		{"../../uihost/uinoderender", repoBlobURL + "framework/uihost/uinoderender"},
		{"../../../battery/admin/", repoBlobURL + "battery/admin"},
		{"../../../core-ui/ARCHITECTURE.md#islands", repoBlobURL + "core-ui/ARCHITECTURE.md#islands"},
		{"helpers.go", repoBlobURL + "framework/docs/content/helpers.go"},
		// Left as written: no page to rewrite it to.
		{"../../../../outside.md", ""},
		{"/docs/a2a", ""},
		{"#tasks", ""},
		{"https://example.com/a.md", ""},
		{"mailto:a@b.c", ""},
		{"", ""},
	} {
		got, ok := siteDocHref(tc.in)
		if !ok {
			got = ""
		}
		if got != tc.want {
			t.Errorf("siteDocHref(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Markup quoted in a doc's code is escaped by the renderer, so only
	// real attributes are rewritten.
	md := string(ui.Markdown(ui.MarkdownConfig{Source: "[A](a2a.md) and `<a href=\"a2a.md\">`"}))
	got := siteDocLinks(md)
	if !strings.Contains(got, `href="/docs/a2a"`) || strings.Count(got, "/docs/a2a") != 1 {
		t.Errorf("rewrite touched quoted code or missed the link:\n%s", got)
	}
}
