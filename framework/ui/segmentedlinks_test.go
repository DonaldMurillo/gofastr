package ui

import (
	"strings"
	"testing"
)

func TestSegmentedLinks(t *testing.T) {
	h := string(SegmentedLinks(SegmentedLinksConfig{Label: "Layout", IconOnly: true, Items: []SegmentLink{
		{Text: "Table", Icon: "list", Href: "/x?as=table", Current: true},
		{Text: "Cards", Icon: "grid", Href: "/x?as=cards"},
	}}))
	for _, want := range []string{`role="group"`, `aria-label="Layout"`, `aria-current="true"`, `aria-label="Cards"`, `href="/x?as=cards"`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q: %s", want, h)
		}
	}
	if strings.Count(h, "aria-current") != 1 {
		t.Errorf("want one current segment: %s", h)
	}
}
