package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestEntryListRanksFromStart(t *testing.T) {
	h := string(EntryList(EntryListConfig{
		Label:  "Top stories",
		Ranked: true,
		Start:  31,
		Entries: []Entry{
			{Title: "First", Href: "https://example.com/a", Source: "example.com"},
			{Title: "Second", Href: "/item/2"},
		},
	}))
	for _, want := range []string{
		`<ol `, `aria-label="Top stories"`, `start="31"`,
		`href="https://example.com/a"`, `>First</a>`, `>example.com</span>`,
		`href="/item/2"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("EntryList missing %q:\n%s", want, h)
		}
	}
	if n := strings.Count(h, `<li `); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

func TestEntryListUnrankedIsAPlainList(t *testing.T) {
	h := string(EntryList(EntryListConfig{
		Label:   "Saved",
		Entries: []Entry{{Title: "One", Href: "/1"}},
	}))
	if !strings.HasPrefix(h, `<ul `) || strings.Contains(h, `start=`) {
		t.Errorf("unranked list should be a <ul> with no start:\n%s", h)
	}
}

func TestEntryListMetaIsCallerMarkup(t *testing.T) {
	h := string(EntryList(EntryListConfig{
		Label: "Top",
		Entries: []Entry{{
			Title: "T", Href: "/t", ID: "e1",
			Meta: render.HTML(`12 points by <a href="/user/ada">ada</a>`),
		}},
	}))
	if !strings.Contains(h, `<a href="/user/ada">ada</a>`) {
		t.Errorf("meta markup not passed through:\n%s", h)
	}
	if !strings.Contains(h, `id="e1"`) {
		t.Errorf("entry id missing:\n%s", h)
	}
}

func TestEntryListEscapesTitleAndScheme(t *testing.T) {
	h := string(EntryList(EntryListConfig{
		Label:   "Top",
		Entries: []Entry{{Title: `<b>x</b>`, Href: "javascript:alert(1)"}},
	}))
	if strings.Contains(h, "<b>x</b>") {
		t.Errorf("title not escaped:\n%s", h)
	}
	if strings.Contains(h, "javascript:") {
		t.Errorf("unsafe scheme kept:\n%s", h)
	}
}

func TestEntryListRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("EntryList without Label did not panic")
		}
	}()
	EntryList(EntryListConfig{Entries: []Entry{{Title: "x", Href: "/x"}}})
}

func TestEntryListSheetUsesTokens(t *testing.T) {
	css := entryListCSS(style.Theme{})
	for _, want := range []string{"::marker", "var(--color-text-muted", "--ui-entry-list-rank-width"} {
		if !strings.Contains(css, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
	// A grid or flex <li> draws no ::marker, which is the rank.
	item := css[strings.Index(css, ".fui-entry-list__item {"):]
	item = item[:strings.Index(item, "}")]
	if strings.Contains(item, "display:") {
		t.Errorf("item rule changes display, which drops the rank marker:\n%s", item)
	}
}

// The unranked list drops its markers, so it says role="list" to keep
// its semantics in WebKit/VoiceOver; the ranked list keeps decimal
// markers and needs no role.
func TestEntryListKeepsListRole(t *testing.T) {
	entries := []Entry{{Title: "a", Href: "/a"}}
	if h := string(EntryList(EntryListConfig{Label: "L", Entries: entries})); !strings.Contains(h, `role="list"`) {
		t.Errorf("unranked list has no role=list:\n%s", h)
	}
	if h := string(EntryList(EntryListConfig{Label: "L", Entries: entries, Ranked: true})); strings.Contains(h, `role="list"`) {
		t.Errorf("ranked list carries a redundant role=list:\n%s", h)
	}
}
