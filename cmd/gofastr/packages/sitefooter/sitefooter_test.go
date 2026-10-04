package sitefooter_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/packages/sitefooter"
)

func full() sitefooter.Config {
	return sitefooter.Config{
		Name:    "Example",
		Tagline: "One line about the app.",
		Columns: []sitefooter.Column{
			{Title: "Product", Links: []sitefooter.Link{{Label: "Overview", Href: "/"}}},
			{Title: "Company", Links: []sitefooter.Link{{Label: "About", Href: "/about"}}},
		},
		Note: "© 2026 Example",
	}
}

func TestRenderFooterLandmarks(t *testing.T) {
	html := string(sitefooter.Render(full()))
	for _, want := range []string{
		// The contentinfo landmark, owned by this package's style.
		"<footer", `role="contentinfo"`, `data-cui-scope="sitefooter"`,
		// The measure-taking inner, the lead-over-columns top, the note.
		`class="inner"`, `class="top"`, `class="columns"`, `class="note"`,
		// The brand links home; the columns are titled lists.
		`class="brand"`, `href="/"`, "Example",
		`class="title"`, "Product", `class="links"`, `href="/about"`,
		"© 2026 Example",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered footer missing %q:\n%s", want, html)
		}
	}
}

func TestRenderTaglineAndNoteOptional(t *testing.T) {
	cfg := full()
	cfg.Tagline, cfg.Note = "", ""
	html := string(sitefooter.Render(cfg))
	for _, banned := range []string{`class="tagline"`, `class="note"`, "One line about the app.", "© 2026"} {
		if strings.Contains(html, banned) {
			t.Errorf("empty Tagline/Note should draw neither (%q present):\n%s", banned, html)
		}
	}

	with := string(sitefooter.Render(full()))
	for _, want := range []string{`class="tagline"`, "One line about the app.", `class="note"`} {
		if !strings.Contains(with, want) {
			t.Errorf("Tagline and Note should draw (%q missing):\n%s", want, with)
		}
	}
}
