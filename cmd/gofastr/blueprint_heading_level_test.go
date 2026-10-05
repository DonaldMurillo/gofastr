package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A dashboard's chart and card titles sit directly under the page's h1, but
// ui.Card defaults its heading to h3: every generated dashboard skipped h2 and
// failed axe heading-order (meridian patched HeadingLevel: 2 in by hand). A
// card with no titled ancestor now says HeadingLevel: 2; one inside a titled
// section keeps the default h3 under the section's h2.
func TestDashboardCardTitlesAreH2(t *testing.T) {
	crudOn := true
	chart := func(title string) BlueprintBlock {
		return BlueprintBlock{Kind: "bar_chart", Props: map[string]any{
			"title":  title,
			"source": map[string]any{"entity": "tickets", "group_by": "status"},
		}}
	}
	bp := Blueprint{
		App: BlueprintApp{Name: "Dash", Module: "example.com/dash"},
		Entities: []framework.EntityDeclaration{{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "tickets", Exposure: &framework.ExposureDeclaration{CRUD: &crudOn}, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
		}}},
		Screens: []BlueprintScreen{{
			Name:  "dashboard",
			Route: "/",
			Body: []BlueprintBlock{
				{Kind: "stat_row", Children: []BlueprintBlock{{Kind: "stat_card", Props: map[string]any{"label": "Open", "value": "3"}}}},
				chart("Top chart"),
				{Kind: "card", Props: map[string]any{"heading": "Top card", "text": "x"}},
				{Kind: "section", Props: map[string]any{"heading": "Breakdown"}, Children: []BlueprintBlock{
					chart("Nested chart"),
					{Kind: "card", Props: map[string]any{"heading": "Nested card", "text": "y"}},
				}},
			},
		}},
	}
	screen := filesByName(mustRenderBlueprintFiles(t, bp))["screen_dashboard.go"]
	if screen == "" {
		t.Fatal("no screen_dashboard.go rendered")
	}
	for _, want := range []string{
		`ui.CardConfig{Heading: "Top chart", HeadingLevel: 2}`,
		`ui.CardConfig{Heading: "Top card", Description: "x", HeadingLevel: 2}`,
		`ui.CardConfig{Heading: "Nested chart"}`,
		`ui.CardConfig{Heading: "Nested card", Description: "y"}`,
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen_dashboard.go is missing %s\n%s", want, screen)
		}
	}
}
