package sitesearch

import (
	"strings"
	"testing"
)

func extract(t *testing.T, url, doc string) Page {
	t.Helper()
	p, ok, err := Extract(url, strings.NewReader(doc))
	if err != nil || !ok {
		t.Fatalf("Extract(%s) = ok %v, err %v", url, ok, err)
	}
	return p
}

const formsDoc = `<!doctype html><html><head><title>Forms · site</title></head><body>
<header><nav><a href="/">Home</a> Navigation chrome</nav></header>
<main>
  <nav aria-label="Docs">Sidebar links widget</nav>
  <article>
    <h1>Forms <a href="#forms" aria-hidden="true">#</a></h1>
    <p>Build a form from a <code>FormField</code> list.</p>
    <h2 id="validation">Validation</h2>
    <p>Errors render beside each field.</p>
    <div data-search-ignore><p>Live demo output ignoreme</p></div>
    <h3 id="server-errors">Server errors</h3>
    <p>The server returns a field map.</p>
    <dialog><h2>Confirm</h2><p>dialogtext</p></dialog>
    <div hidden>hiddentext</div>
  </article>
</main>
<footer>Footer colophon</footer>
</body></html>`

func TestExtractSkipsChrome(t *testing.T) {
	p := extract(t, "/docs/forms", formsDoc)
	if p.Title != "Forms" {
		t.Errorf("Title = %q, want Forms (the h1, no permalink glyph)", p.Title)
	}
	want := []Section{
		{Text: "Build a form from a FormField list."},
		{ID: "validation", Heading: "Validation", Text: "Errors render beside each field."},
		{ID: "server-errors", Heading: "Server errors", Text: "The server returns a field map."},
	}
	if len(p.Sections) != len(want) {
		t.Fatalf("Sections = %+v, want %+v", p.Sections, want)
	}
	for i := range want {
		if p.Sections[i] != want[i] {
			t.Errorf("Sections[%d] = %+v, want %+v", i, p.Sections[i], want[i])
		}
	}
}

func TestExtractNoindex(t *testing.T) {
	doc := `<html><head><meta name="robots" content="NOINDEX, follow"></head><body><main><h1>X</h1></main></body></html>`
	if _, ok, _ := Extract("/x", strings.NewReader(doc)); ok {
		t.Error("a noindex page was indexed")
	}
}

func TestExtractTitleFallback(t *testing.T) {
	p := extract(t, "/x", `<html><head><title>Only title</title></head><body><main><p>body</p></main></body></html>`)
	if p.Title != "Only title" {
		t.Errorf("Title = %q", p.Title)
	}
}

func TestIndexTermsSplitCamelCase(t *testing.T) {
	got := strings.Join(indexTerms("CommandPalette"), ",")
	if got != "commandpalette,command,palette" {
		t.Errorf("indexTerms(CommandPalette) = %s", got)
	}
	got = strings.Join(indexTerms("HTMLParser2"), ",")
	if got != "htmlparser2,html,parser" {
		t.Errorf("indexTerms(HTMLParser2) = %s", got)
	}
}

func corpus() []Page {
	return []Page{
		{URL: "/a", Title: "Tables", Sections: []Section{{Text: "Render rows. Palette mentioned once."}}},
		{URL: "/b", Title: "Command palette", Sections: []Section{{Text: "Open it with a shortcut."}}},
		{URL: "/c", Title: "Overlays", Sections: []Section{
			{Text: "Dialogs and sheets."},
			{ID: "palette", Heading: "Palette overlay", Text: "The overlay a palette opens, quokka."},
		}},
	}
}

func urls(rs []Result) string {
	var u []string
	for _, r := range rs {
		u = append(u, r.URL)
	}
	return strings.Join(u, " ")
}

func TestSearchRanksTitleOverHeadingOverBody(t *testing.T) {
	idx := Build(corpus())
	if got := urls(idx.Search("palette", 10)); got != "/b /c /a" {
		t.Errorf("palette ranked %q, want /b /c /a", got)
	}
}

func TestSearchAndsTerms(t *testing.T) {
	idx := Build(corpus())
	if got := urls(idx.Search("palette overlay", 10)); got != "/c" {
		t.Errorf("palette overlay = %q, want /c", got)
	}
	if got := idx.Search("palette zebra", 10); got != nil {
		t.Errorf("an unmatched term still returned %q", urls(got))
	}
}

func TestSearchPrefixesTerms(t *testing.T) {
	idx := Build(corpus())
	if got := urls(idx.Search("pal", 10)); got != "/b /c /a" {
		t.Errorf("pal = %q", got)
	}
	if got := idx.Search("p", 10); got != nil {
		t.Errorf("a one-rune query matched %q", urls(got))
	}
}

func TestSearchDeepLinksSections(t *testing.T) {
	idx := Build(corpus())
	rs := idx.Search("overlay", 10)
	if len(rs) != 1 || len(rs[0].Sections) != 1 || rs[0].Sections[0].ID != "palette" {
		t.Fatalf("overlay = %+v, want /c with the #palette section", rs)
	}
	// Only the section matched: its row shows the hit, the page row
	// does not repeat it.
	rs = idx.Search("quokka", 10)
	if len(rs) != 1 || len(rs[0].Sections) != 1 {
		t.Fatalf("quokka = %+v", rs)
	}
	if rs[0].Excerpt != nil {
		t.Errorf("page excerpt = %+v, want none", rs[0].Excerpt)
	}
}

func TestExcerptMarksHits(t *testing.T) {
	text := strings.Repeat("lead word ", 10) + "the palette opens " + strings.Repeat("tail word ", 30)
	segs := excerpt(strings.TrimSpace(text), []string{"pal"})
	var b strings.Builder
	for _, s := range segs {
		if s.Hit {
			b.WriteString("[" + s.Text + "]")
		} else {
			b.WriteString(s.Text)
		}
	}
	got := b.String()
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("a cut window lost its ellipses: %q", got)
	}
	if !strings.Contains(got, "the [palette] opens") {
		t.Errorf("the hit is not marked: %q", got)
	}
	if strings.Contains(got, "…ead") || strings.Contains(got, "wor…") {
		t.Errorf("the window cut a word: %q", got)
	}
}

func TestSearchMatchesSingularOfPlural(t *testing.T) {
	idx := Build([]Page{
		{URL: "/form", Title: "Form module", Sections: []Section{{}}},
		{URL: "/class", Title: "Class names", Sections: []Section{{}}},
	})
	if got := urls(idx.Search("forms", 5)); got != "/form" {
		t.Errorf("forms = %q, want /form", got)
	}
	if got := idx.Search("classs", 5); got != nil {
		t.Errorf("a double-s term was stemmed: %q", urls(got))
	}
}

func TestSearchLinksOnlyHeadingsWithIDs(t *testing.T) {
	idx := Build([]Page{{URL: "/p", Title: "Page", Sections: []Section{
		{},
		{Heading: "Unlinked zebra"},
		{ID: "linked", Heading: "Linked zebra"},
	}}})
	rs := idx.Search("zebra", 5)
	if len(rs) != 1 || len(rs[0].Sections) != 1 || rs[0].Sections[0].ID != "linked" {
		t.Errorf("zebra = %+v, want one section row, #linked", rs)
	}
}

func TestBuildDropsDuplicatePages(t *testing.T) {
	same := []Section{{Text: "identical text"}}
	idx := Build([]Page{
		{URL: "/a", Title: "Twin", Sections: same},
		{URL: "/b", Title: "Twin", Sections: same},
		{URL: "/a", Title: "Other", Sections: []Section{{Text: "other"}}},
	})
	if idx.Len() != 1 {
		t.Errorf("Len = %d, want 1 (a twin and a repeated URL dropped)", idx.Len())
	}
}
