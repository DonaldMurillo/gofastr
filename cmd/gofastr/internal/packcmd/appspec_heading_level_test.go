package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// A dashboard's chart and card titles sit directly under the page's h1, but
// ui.Card defaults its heading to h3: every generated dashboard skipped h2 and
// failed axe heading-order (meridian patched HeadingLevel: 2 in by hand). A
// card with no titled ancestor now says HeadingLevel: 2; one inside a titled
// section keeps the default h3 under the section's h2.
func TestDashboardCardTitlesAreH2(t *testing.T) {
	crudOn := true
	chart := func(title string) appspec.AppSpecBlock {
		return appspec.AppSpecBlock{Kind: "bar_chart", Props: map[string]any{
			"title":  title,
			"source": map[string]any{"entity": "tickets", "group_by": "status"},
		}}
	}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Dash", Module: "example.com/dash"},
		Entities: []framework.EntityDeclaration{{Scope: &framework.ScopeDeclaration{}, Pagination: &framework.PaginationDeclaration{}, Name: "tickets", Exposure: &framework.ExposureDeclaration{CRUD: &crudOn}, Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
		}}},
		Screens: []appspec.AppSpecScreen{{
			Name:  "dashboard",
			Route: "/",
			Body: []appspec.AppSpecBlock{
				{Kind: "stat_row", Children: []appspec.AppSpecBlock{{Kind: "stat_card", Props: map[string]any{"label": "Open", "value": "3"}}}},
				chart("Top chart"),
				{Kind: "card", Props: map[string]any{"heading": "Top card", "text": "x"}},
				{Kind: "section", Props: map[string]any{"heading": "Breakdown"}, Children: []appspec.AppSpecBlock{
					chart("Nested chart"),
					{Kind: "card", Props: map[string]any{"heading": "Nested card", "text": "y"}},
				}},
			},
		}},
	}
	screen := inlinePhrases(filesByName(mustRenderAppSpecFiles(t, bp))["screen_dashboard.go"])
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
