package ui

import (
	"strings"
	"testing"
)

func TestTabNavRendersLabelledNavOfLinks(t *testing.T) {
	h := string(TabNav(TabNavConfig{
		Label: "Views",
		Items: []TabNavItem{
			{Text: "All", Href: "/orders"},
			{Text: "Open", Href: "/orders?view=open"},
		},
	}))
	for _, want := range []string{
		`<nav`, `aria-label="Views"`, `role="navigation"`,
		`href="/orders"`, `href="/orders?view=open"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("TabNav missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, `aria-current`) {
		t.Errorf("no Current item, but aria-current rendered:\n%s", h)
	}
}

func TestTabNavMarksExactlyTheCurrentLink(t *testing.T) {
	h := string(TabNav(TabNavConfig{
		Label: "Views",
		Items: []TabNavItem{
			{Text: "All", Href: "/orders"},
			{Text: "Open", Href: "/orders?view=open", Current: true},
		},
	}))
	if n := strings.Count(h, `aria-current="page"`); n != 1 {
		t.Fatalf("aria-current count = %d, want 1:\n%s", n, h)
	}
	// Attributes render sorted within the tag, so the mark and the href
	// share one opener when they share one link.
	if !strings.Contains(h, `<a aria-current="page" class="fui-tab-nav__link" data-cui-internal="" href="/orders?view=open"`) {
		t.Errorf("aria-current is not on the Open link:\n%s", h)
	}
	if strings.Contains(h, `<a aria-current="page" class="fui-tab-nav__link" data-cui-internal="" href="/orders"`) {
		t.Errorf("the All link carries the current mark too:\n%s", h)
	}
}

func TestTabNavBadgeIsInsideTheLinkAndHiddenFromAT(t *testing.T) {
	h := string(TabNav(TabNavConfig{
		Label: "Views",
		Items: []TabNavItem{{Text: "Open", Href: "/x", Badge: "12"}},
	}))
	open := strings.Index(h, "<a ")
	closeA := strings.Index(h, "</a>")
	badge := strings.Index(h, "fui-tab-nav__badge")
	if open < 0 || closeA < 0 || badge < open || badge > closeA {
		t.Fatalf("badge must render inside the anchor:\n%s", h)
	}
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("badge is not aria-hidden; the link's name must stay the label:\n%s", h)
	}
}

func TestTabNavRefusesBrokenConfiguration(t *testing.T) {
	cases := map[string]func(){
		"no label": func() { TabNav(TabNavConfig{Items: []TabNavItem{{Text: "A", Href: "/a"}}}) },
		"no items": func() { TabNav(TabNavConfig{Label: "Views"}) },
		"no text":  func() { TabNav(TabNavConfig{Label: "V", Items: []TabNavItem{{Href: "/a"}}}) },
		"no href":  func() { TabNav(TabNavConfig{Label: "V", Items: []TabNavItem{{Text: "A"}}}) },
		"two current": func() {
			TabNav(TabNavConfig{Label: "V", Items: []TabNavItem{{Text: "A", Href: "/a", Current: true}, {Text: "B", Href: "/b", Current: true}}})
		},
	}
	for name, fn := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
}

func TestTabNavExtraAttrsOnRoot(t *testing.T) {
	h := TabNav(TabNavConfig{
		Label:      "Views",
		Items:      []TabNavItem{{Text: "A", Href: "/a"}},
		ExtraAttrs: map[string]string{"data-test": "hook", "aria-label": "evil", "class": "evil"},
	})
	out := string(h)
	if !strings.Contains(out, `data-test="hook"`) {
		t.Errorf("test hook dropped:\n%s", out)
	}
	if strings.Contains(out, `aria-label="evil"`) || strings.Contains(out, `class="evil"`) {
		t.Errorf("owned attribute overridden via ExtraAttrs:\n%s", out)
	}
}
