package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestDenseRecordSemantics(t *testing.T) {
	row := string(Card(CardConfig{Variant: CardRow, Href: "/issues/1", Heading: "ISS-1", Description: "Fix invoice"}, render.Text("Open")))
	for _, want := range []string{`href="/issues/1"`, `<h3`, "ISS-1", "Fix invoice", "Open"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row lost %q: %s", want, row)
		}
	}
	fields := string(DetailList(DetailListConfig{Inline: true, Items: []DetailItem{{Label: "Assignee", Value: render.Text("Ada")}}}))
	if !strings.Contains(fields, "<dt") || !strings.Contains(fields, "<dd") {
		t.Fatalf("inline fields lost description semantics: %s", fields)
	}
}

func TestCompactComponentsKeepAccessibleNames(t *testing.T) {
	cases := []struct {
		name string
		body render.HTML
		want string
	}{
		{"title", PageHeader(PageHeaderConfig{Title: "Article", Compact: true}), "Article"},
		{"section", Section(SectionConfig{Heading: "Release", Compact: true}), `aria-labelledby="release-title"`},
		{"toolbar", Toolbar(ToolbarConfig{Label: "Record actions", Plain: true, Groups: []ToolbarGroup{{Children: []render.HTML{Button(ButtonConfig{Label: "Save"})}}}}), `aria-label="Record actions"`},
		{"banner", Banner(BannerConfig{Title: "Released", Strip: true}), `role="status"`},
		{"filter", FilterToolbar(FilterToolbarConfig{Action: "/records", Compact: true, Search: &FilterSearch{Name: "q", Value: "invoice"}}), `method="GET"`},
		{"paragraphs", Stack(StackConfig{TrimMargins: true}, render.Tag("p", nil, render.Text("Description"))), "<p>Description</p>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(string(c.body), c.want) {
				t.Fatalf("lost semantic contract: %s", c.body)
			}
		})
	}
}
