package sitesearch

import (
	"io"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
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

// "none" is noindex plus nofollow, and only a whole directive counts:
// the substring test kept a "none" page in the index.
func TestExtractRobotsNoneDrops(t *testing.T) {
	for content, indexed := range map[string]bool{
		"none":            false,
		"NONE":            false,
		"nofollow, none":  false,
		"noindex":         false,
		"all":             true,
		"nofollow":        true,
		"max-snippet:-1":  true,
		"index, nofollow": true,
	} {
		doc := `<html><head><meta name="robots" content="` + content + `"></head><body><main><h1>X</h1></main></body></html>`
		if _, ok, _ := Extract("/x", strings.NewReader(doc)); ok != indexed {
			t.Errorf("robots %q: indexed = %v, want %v", content, ok, indexed)
		}
	}
}

// A robots tag inside content the page keeps out of the index (sample
// markup in a docs page, a demo) does not drop the page.
func TestExtractIgnoredNoindexKeepsPage(t *testing.T) {
	doc := `<html><body><main><h1>Robots</h1><pre data-search-ignore><meta name="robots" content="noindex"></pre></main></body></html>`
	for name, extract := range map[string]func(string, io.Reader) (Page, bool, error){"Extract": Extract, "hosted": extractHosted} {
		if _, ok, _ := extract("/x", strings.NewReader(doc)); !ok {
			t.Errorf("%s: an ignored robots tag dropped the page", name)
		}
	}
}

// A <main> under a hidden or ignored ancestor is not the page's: taking
// it as the root started the walk below the ancestor that hides it,
// indexing its text and dropping the visible <main>. A hidden <main>
// itself indexed nothing and dropped the visible one too.
func TestExtractSkipsHiddenMain(t *testing.T) {
	for _, hidden := range []string{
		`<div hidden><main>secretword</main></div>`,
		`<div data-search-ignore><main>secretword</main></div>`,
		`<section aria-hidden="true"><main>secretword</main></section>`,
		`<main hidden>secretword</main>`,
	} {
		doc := `<html><body>` + hidden + `<main>publicword</main></body></html>`
		p := extract(t, "/x", doc)
		var text strings.Builder
		for _, s := range p.Sections {
			text.WriteString(s.Text + " ")
		}
		if got := text.String(); strings.Contains(got, "secretword") || !strings.Contains(got, "publicword") {
			t.Errorf("%s: indexed %q", hidden, got)
		}
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

func marked(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Hit {
			b.WriteString("[" + s.Text + "]")
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// A page titled with the query outranks pages that only repeat it, and
// a shorter title that holds it outranks a longer one.
func TestSearchTitleNamesTheAnswer(t *testing.T) {
	many := strings.Repeat("Every table has rows. ", 5)
	idx := Build([]Page{
		{URL: "/migrations", Title: "Migrations", Sections: []Section{{Text: many}, {ID: "t", Heading: "Table changes", Text: many}}},
		{URL: "/toc", Title: "TableOfContents", Sections: []Section{{Text: "A table of contents. " + many}}},
		{URL: "/datatable", Title: "DataTable", Sections: []Section{{Text: "Rows and columns."}}},
	})
	if got := urls(idx.Search("table", 3)); got != "/datatable /toc /migrations" {
		t.Errorf("table: %s, want /datatable /toc /migrations", got)
	}
	if got := urls(idx.Search("data table", 3)); got != "/datatable" {
		t.Errorf("data table: %s, want /datatable", got)
	}
}

// A title word the query only begins earns half its slot, so a title
// that names the word whole ranks first.
func TestSearchTitlePrefixEarnsLess(t *testing.T) {
	idx := Build([]Page{
		{URL: "/builder", Title: "formbuilder", Sections: []Section{{}}},
		{URL: "/form", Title: "Form module", Sections: []Section{{}}},
	})
	if got := urls(idx.Search("form", 2)); got != "/form /builder" {
		t.Errorf("form: %s, want /form /builder", got)
	}
}

// The excerpt opens on the first whole-word hit, never mid-word, even
// when an earlier hit sits inside a URL with no space before it.
func TestExcerptOpensOnWholeWord(t *testing.T) {
	text := "Install github.com/DonaldMurillo/gofastr-plugins/formbuilder today. " +
		strings.Repeat("Filler words here. ", 10) + "A form collects fields."
	got := marked(excerpt(text, []string{"form"}))
	if !strings.HasPrefix(got, "…") || strings.Contains(got, "ithub") {
		t.Errorf("excerpt %q, want it to open on a word", got)
	}
	if !strings.Contains(got, "A [form] collects") {
		t.Errorf("excerpt %q does not show the whole-word hit", got)
	}
	got = marked(excerpt(strings.Repeat("lead ", 12)+"https://example.com/really/long/path/to/formbuilder now", []string{"formbuilder"}))
	if !strings.HasPrefix(got, "…[formbuilder]") {
		t.Errorf("excerpt %q, want it to open on the hit when the lead-in has no space", got)
	}
}

func TestExcerptMarksSingularOfPlural(t *testing.T) {
	if got := marked(excerpt("Build a form.", []string{"forms"})); got != "Build a [form]." {
		t.Errorf("excerpt %q, want the singular marked", got)
	}
}

// A term also finds its -ing form as a whole word: "theme" names the
// Theming page before ThemePicker, which only holds it in a longer title.
func TestSearchMatchesGerund(t *testing.T) {
	idx := Build([]Page{
		{URL: "/picker", Title: "ThemePicker", Sections: []Section{{Text: "Pick a theme. theme theme."}}},
		{URL: "/theming", Title: "Theming", Sections: []Section{{Text: "Tokens drive caching and colors."}}},
	})
	if got := urls(idx.Search("theme", 2)); got != "/theming /picker" {
		t.Errorf("theme: %s, want /theming /picker", got)
	}
	if got := urls(idx.Search("cache", 2)); got != "/theming" {
		t.Errorf("cache: %s, want /theming", got)
	}
	if got := marked(excerpt("Tokens drive caching.", []string{"cache"})); got != "Tokens drive [caching]." {
		t.Errorf("excerpt %q, want the -ing form marked", got)
	}
}

// A camelCase title named whole earns the full title bonus and beats
// a longer title the query only begins: its parts do not count as
// extra title words.
func TestSearchCamelTitleNamedWhole(t *testing.T) {
	idx := Build([]Page{
		{URL: "/plural", Title: "Datatables", Sections: []Section{{}}},
		{URL: "/datatable", Title: "DataTable", Sections: []Section{{}}},
	})
	res := idx.Search("datatable", 2)
	if got := urls(res); got != "/datatable /plural" {
		t.Fatalf("datatable: %s, want /datatable /plural", got)
	}
	if res[0].Score < titleBonus {
		t.Errorf("DataTable scored %d, want the full title bonus %d", res[0].Score, titleBonus)
	}
}

// A word the title repeats is one slot, so "Form, form" is named whole
// by form.
func TestSearchTitleWordRepeatsOnce(t *testing.T) {
	idx := Build([]Page{{URL: "/form", Title: "Form, form", Sections: []Section{{}}}})
	res := idx.Search("form", 1)
	if len(res) != 1 || res[0].Score < titleBonus {
		t.Errorf("form: %+v, want /form with the full title bonus", res)
	}
}

// A term the title holds twice takes its widest span: in "DataTable
// MyDatatable", datatable names DataTable's two slots whole, not just
// MyDatatable's second part.
func TestSearchTitleTermTakesWidestSpan(t *testing.T) {
	idx := Build([]Page{{URL: "/t", Title: "DataTable MyDatatable", Sections: []Section{{}}}})
	res := idx.Search("datatable", 1)
	if len(res) != 1 || res[0].Score < titleBonus/2 {
		t.Errorf("datatable: %+v, want half the title bonus (2 of 4 slots)", res)
	}
}

// Two query terms that begin the same title word earn it once.
func TestSearchTermsShareTitleWordOnce(t *testing.T) {
	idx := Build([]Page{
		{URL: "/formatter", Title: "Formatter", Sections: []Section{{}}},
		{URL: "/options", Title: "Form Format Options", Sections: []Section{{}}},
	})
	if got := urls(idx.Search("form format", 2)); got != "/options /formatter" {
		t.Errorf("form format: %s, want /options /formatter", got)
	}
}

// The excerpt anchor is the first word any query term names whole,
// whatever order the terms came in.
func TestExcerptAnchorIgnoresTermOrder(t *testing.T) {
	text := "formbuilder " + strings.Repeat("filler ", 40) + "forms."
	for _, terms := range [][]string{{"form", "forms"}, {"forms", "form"}} {
		if got := marked(excerpt(text, terms)); !strings.Contains(got, "[forms]") {
			t.Errorf("terms %q: excerpt %q, want it on forms", terms, got)
		}
	}
}

// SearchPage pages through the ranked results and counts them all.
func TestSearchPageOffsetsAndCounts(t *testing.T) {
	var pages []Page
	for i := range 7 {
		pages = append(pages, Page{URL: "/p/" + string(rune('a'+i)), Title: "Walrus " + string(rune('a'+i)),
			Sections: []Section{{Text: strings.Repeat("walrus ", i+1)}}})
	}
	idx := Build(pages)
	all := idx.Search("walrus", 10)
	if len(all) != 7 {
		t.Fatalf("%d results, want 7", len(all))
	}
	for _, c := range []struct{ offset, limit, want int }{
		{0, 3, 3}, {3, 3, 3}, {6, 3, 1}, {7, 3, 0}, {100, 3, 0}, {-2, 3, 3}, {2, math.MaxInt, 5},
	} {
		got, total := idx.SearchPage("walrus", c.offset, c.limit)
		if total != 7 || len(got) != c.want {
			t.Errorf("offset %d limit %d: %d results of %d, want %d of 7", c.offset, c.limit, len(got), total, c.want)
			continue
		}
		from := max(c.offset, 0)
		for i, r := range got {
			if r.URL != all[from+i].URL {
				t.Errorf("offset %d: result %d is %s, want %s", c.offset, i, r.URL, all[from+i].URL)
			}
		}
	}
	if _, total := idx.SearchPage("zzz", 0, 3); total != 0 {
		t.Errorf("no match counted %d", total)
	}
}

// Section text kept for excerpts ends on a whole word and says it was
// cut, so an excerpt near the end never stops mid-word.
func TestCappedTextEndsOnWord(t *testing.T) {
	text := strings.Repeat("walrus ", 400) // 2800 runes
	idx := Build([]Page{{URL: "/w", Title: "W", Sections: []Section{{Text: text}}}})
	kept := idx.pages[0].Sections[0].Text
	if !strings.HasSuffix(kept, "walrus…") {
		t.Errorf("kept text ends %q, want a whole word and …", kept[len(kept)-12:])
	}
	if n := utf8.RuneCountInString(kept); n > excerptRunes {
		t.Errorf("kept %d runes, cap %d", n, excerptRunes)
	}
	if got := capRunes("short text", 20); got != "short text" {
		t.Errorf("uncut text changed to %q", got)
	}
	if got := capRunes(strings.Repeat("x", 30), 20); got != strings.Repeat("x", 19)+"…" {
		t.Errorf("one long word cut to %q", got)
	}
}

// Every result surface links BASE + URL, so a URL that is not a site
// path never enters the index: no scheme, no other host.
func TestBuildDropsOffSiteURLs(t *testing.T) {
	var pages []Page
	for _, u := range []string{"javascript:alert(1)", "//evil.example/x", "https://evil.example/x", `/\evil.example`, "docs/forms", "/\t/evil.example", "/\n/evil.example", "/\r/evil.example", "/x\x00", "/x\x7f", "/ok"} {
		pages = append(pages, Page{URL: u, Title: "Walrus " + u, Sections: []Section{{Text: "walrus " + u}}})
	}
	idx := Build(pages)
	if idx.Len() != 1 {
		t.Fatalf("indexed %d pages, want only /ok", idx.Len())
	}
	if rs := idx.Search("walrus", 10); len(rs) != 1 || rs[0].URL != "/ok" {
		t.Fatalf("results = %+v", rs)
	}
}

// A page passed to Build with a title and no sections is found by its
// title: title terms were indexed only inside the sections loop.
func TestBuildIndexesTitleOnlyPage(t *testing.T) {
	idx := Build([]Page{{URL: "/a", Title: "Zanzibar"}})
	if rs := idx.Search("zanzibar", 10); len(rs) != 1 || rs[0].URL != "/a" {
		t.Fatalf("results = %+v", rs)
	}
}
