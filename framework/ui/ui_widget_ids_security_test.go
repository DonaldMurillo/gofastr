package ui_test

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: two component instances on one page must never emit the same
// DOM id. An id is the anchor target, the label[for] target, and the
// aria-labelledby target; a duplicate silently re-points every one of
// those at the first occurrence.

// TestSlugDerivedIDsCollideOnRepeat pins the slug-derived half of that
// property across every framework/ui surface that manufactures a DOM id
// by slugging caller text with no disambiguation. The inputs are the
// record-derived strings real screens feed these widgets (section
// headings per category/record, facet option labels per dynamic filter
// value), and repeats are the normal case there, not the pathological
// one. Production currently emits the collisions verbatim — this test
// is the red for the root (a repeated input must yield a distinct id,
// e.g. by suffixing, the way autoID does).
func TestSlugDerivedIDsCollideOnRepeat(t *testing.T) {
	surfaces := []struct {
		name string
		id   string
		page string
	}{
		{
			name: "section-auto-anchor",
			id:   "overview", // section id — in-page rail/scrollspy target
			page: string(ui.Section(ui.SectionConfig{Heading: "Overview"}, render.Text("a"))) +
				string(ui.Section(ui.SectionConfig{Heading: "Overview"}, render.Text("b"))),
		},
		{
			name: "section-heading",
			id:   "ui-section-overview", // h2 id — aria-labelledby target
			page: string(ui.Section(ui.SectionConfig{Heading: "Overview"}, render.Text("a"))) +
				string(ui.Section(ui.SectionConfig{Heading: "Overview"}, render.Text("b"))),
		},
		{
			name: "stepwizard-heading-auto-id",
			id:   "heading-details", // html.Heading auto-slugs its text
			page: string(ui.StepWizard(ui.StepWizardConfig{Action: "/a",
				Steps: []ui.StepWizardStep{{Heading: "Details"}}})) +
				string(ui.StepWizard(ui.StepWizardConfig{Action: "/b",
					Steps: []ui.StepWizardStep{{Heading: "Details"}}})),
		},
		{
			name: "filtertoolbar-pill",
			id:   "filter-color-red-car-x", // slug(name)-slug(value-label)
			page: string(ui.FilterToolbar(ui.FilterToolbarConfig{
				Action: "/list",
				Facets: []ui.Facet{{
					Name:  "color",
					Label: "Color",
					Kind:  ui.FacetPills,
					Options: []ui.FacetOption{
						{Value: "Red Car", Label: "x"},
						{Value: "red car", Label: "x"},
					},
				}},
			})),
		},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			if n := strings.Count(s.page, fmt.Sprintf("id=%q", s.id)); n != 1 {
				t.Errorf("SECURITY/RED: %s: duplicate DOM id %q emitted %d times; label[for]/anchor/aria wiring silently resolves to the first occurrence:\n%s", s.name, s.id, n, s.page)
			}
		})
	}
}

// TestAutoIDUniqueAcrossConcurrent pins the generated half: widgets that
// auto-generate ids (helpers.go autoID, a process-global atomic counter)
// must never collide, including when renders race across goroutines the
// way concurrent HTTP requests do. Surfaces: the two framework/ui
// widgets that call autoID — Carousel (wrapper id, also the
// aria-controls target of every nav button and dot) and Repeater (the
// -items region id, also the data-fui-rpc-signal target).
func TestAutoIDUniqueAcrossConcurrent(t *testing.T) {
	const goroutines = 32
	const perG = 4

	carouselIDs := make([][]string, goroutines)
	repeaterIDs := make([][]string, goroutines)
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			cs, rs := make([]string, 0, perG), make([]string, 0, perG)
			for range perG {
				c := string(ui.Carousel(ui.CarouselConfig{
					Label:  "L",
					Slides: []ui.CarouselSlide{{Content: render.Text("s")}},
				}))
				cs = append(cs, extractFirstID(t, c, `ui-carousel-`))
				r := string(ui.Repeater(ui.RepeaterConfig{
					Name:     "tags",
					Template: func(int) render.HTML { return render.Text("t") },
					MinItems: 1,
				}))
				rs = append(rs, extractFirstID(t, r, `rep-`))
			}
			carouselIDs[g], repeaterIDs[g] = cs, rs
		}(g)
	}
	wg.Wait()

	seen := make(map[string]bool)
	for _, ids := range append(carouselIDs, repeaterIDs...) {
		for _, id := range ids {
			if seen[id] {
				t.Errorf("SECURITY: autoID collision: %q generated twice; aria-controls / rpc-signal wiring crosses instances", id)
			}
			seen[id] = true
		}
	}
}

// extractFirstID pulls the first id="prefix…" value out of rendered
// HTML (render.Tag emits attributes with quoted values, sorted).
func extractFirstID(t *testing.T, html, prefix string) string {
	t.Helper()
	re := regexp.MustCompile(`id="(` + regexp.QuoteMeta(prefix) + `[^"]*)"`)
	m := re.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no id with prefix %q found in output:\n%s", prefix, html)
	}
	return m[1]
}
