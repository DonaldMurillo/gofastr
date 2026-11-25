package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Article puts the card in an <article>, and TitleHref makes the title's
// words the card's one link while its action stays a separate control.
func TestCardArticleWithTitleLink(t *testing.T) {
	h := string(Card(CardProps{
		Title: "Release", TitleHref: "/blog/r", Article: true,
		Action: render.HTML(`<button type="button">Share</button>`),
	}, Classes{PartCardTitleLink: "tl"}, render.Text("body")))
	if !strings.HasPrefix(h, "<article") || !strings.HasSuffix(h, "</article>") {
		t.Errorf("an Article card's root is not an <article>:\n%s", h)
	}
	if !strings.Contains(h, `><a class="tl" href="/blog/r">Release</a></h3>`) {
		t.Errorf("the title's words are not the link:\n%s", h)
	}
	if n := strings.Count(h, "<a "); n != 1 {
		t.Errorf("the card holds %d links, want the title's one:\n%s", n, h)
	}
	if !strings.Contains(h, "</h3>") || strings.Index(h, "<button") < strings.Index(h, "</a>") {
		t.Errorf("the action sits inside the title's link:\n%s", h)
	}
	if def := string(Card(CardProps{Title: "Plain"}, nil)); !strings.HasPrefix(def, "<div") {
		t.Errorf("the default card's root changed:\n%s", def)
	}
}

func TestCardTitleLinkRefusals(t *testing.T) {
	for _, c := range []struct {
		name string
		p    CardProps
	}{
		{"with Href", CardProps{Title: "x", Href: "/a", TitleHref: "/b"}},
		{"without a title", CardProps{Desc: "d", TitleHref: "/b"}},
		{"a refused URL", CardProps{Title: "x", TitleHref: "javascript:alert(1)"}},
		{"a linked TitleContent", CardProps{TitleContent: render.HTML(`<A href="/x">x</A>`), TitleHref: "/b"}},
		{"an article with Href", CardProps{Title: "x", Href: "/a", Article: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Card did not panic")
				}
			}()
			Card(c.p, nil)
		})
	}
}

// An <a> may hold no interactive content: the check refuses a link, a
// control, embedded media, a tabindex or contenteditable, and lets
// ordinary phrasing through, <abbr> and <aside> included.
func TestCardTitleLinkAllowsTagsThatStartWithA(t *testing.T) {
	for _, text := range []string{`Set <code>tabindex</code> on it`, `The contenteditable attribute`,
		`<span title="names tabindex here">x</span>`, `<abbr title="a > button">x</abbr>`, `<img src="/i.png" alt="">`} {
		got := string(Card(CardProps{TitleContent: render.HTML(text), TitleHref: "/b"}, nil))
		if !strings.Contains(got, `href="/b"`) {
			t.Errorf("a TitleContent naming an attribute in its text lost its title link:\n%s", got)
		}
	}
	for _, tag := range []string{"abbr", "aside", "article", "area", "b", "em", "span"} {
		got := string(Card(CardProps{TitleContent: render.HTML(`<` + tag + `>x</` + tag + `>`), TitleHref: "/b"}, nil))
		if !strings.Contains(got, `href="/b"`) {
			t.Errorf("a TitleContent holding <%s> lost its title link:\n%s", tag, got)
		}
	}
	for _, link := range []string{`<a href="/x">x</a>`, `<A>x</A>`, "<a\thref=\"/x\">x</a>", `<a/>`,
		`<button type="button">Details</button>`, `<input type="checkbox">`, `<span tabindex="0">x</span>`,
		`<span contenteditable="true">x</span>`, `<audio controls src="/a.mp3"></audio>`,
		`<span tabindex>x</span>`, `<span contenteditable>x</span>`, `<span tabindex/>`, "<span class=\"k\" tabindex\n>x</span>",
		`<span title="1 > 0" tabindex="0">x</span>`, `<SPAN TabIndex="0">x</SPAN>`, `<img src="/m.png" alt="map" usemap="#m">`} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a TitleContent holding %q did not panic", link)
				}
			}()
			Card(CardProps{TitleContent: render.HTML(link), TitleHref: "/b"}, nil)
		}()
	}
}
