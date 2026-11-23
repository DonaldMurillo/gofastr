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
