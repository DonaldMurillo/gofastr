package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Two same-titled sections on one render get distinct ids, and each
// region's aria-labelledby names its own heading.
func TestSameHeadingSectionsGetUniqueIDs(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	a := string(Section(SectionConfig{Heading: "Features", Ctx: ctx}))
	b := string(Section(SectionConfig{Heading: "Features", Ctx: ctx}))
	for _, want := range []string{`id="features"`, `aria-labelledby="features-title"`, `id="features-title"`} {
		if !strings.Contains(a, want) {
			t.Errorf("first section lacks %s:\n%s", want, a)
		}
	}
	for _, want := range []string{`id="features-2"`, `aria-labelledby="features-2-title"`, `id="features-2-title"`} {
		if !strings.Contains(b, want) {
			t.Errorf("second section lacks %s:\n%s", want, b)
		}
	}
	// An explicit id is kept, and a derived one that would repeat it
	// moves aside.
	c := string(Section(SectionConfig{Heading: "Other", ID: "plans", Ctx: ctx}))
	d := string(Section(SectionConfig{Heading: "Plans", Ctx: ctx}))
	if !strings.Contains(c, `id="plans"`) || !strings.Contains(d, `id="plans-2"`) {
		t.Errorf("explicit then derived plans:\n%s\n%s", c, d)
	}
	// No context: today's derived id.
	if e := string(Section(SectionConfig{Heading: "Features"})); !strings.Contains(e, `id="features"`) {
		t.Errorf("no ctx: %s", e)
	}
}

// Two tab strips on one signal name get distinct tab and panel ids.
func TestSameSignalTabsGetUniqueIDs(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	cfg := TabsConfig{SignalName: "view", Tabs: []TabItem{{Label: "One"}, {Label: "Two"}}, Ctx: ctx}
	a, b := string(Tabs(cfg)), string(Tabs(cfg))
	if !strings.Contains(a, `id="view-tab-0"`) || !strings.Contains(a, `aria-controls="view-panel-0"`) {
		t.Errorf("first strip:\n%s", a)
	}
	if !strings.Contains(b, `id="view-2-tab-0"`) || !strings.Contains(b, `aria-controls="view-2-panel-0"`) || !strings.Contains(b, `id="view-2-panel-0"`) {
		t.Errorf("second strip:\n%s", b)
	}
}

var idAttrRe = regexp.MustCompile(` id="([^"]+)"`)

func noDuplicateIDs(t *testing.T, html string) {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range idAttrRe.FindAllStringSubmatch(html, -1) {
		if seen[m[1]] {
			t.Errorf("id %q appears twice:\n%s", m[1], html)
		}
		seen[m[1]] = true
	}
}

// A heading slug that equals another section's heading id moves aside:
// "Features" owns features and features-title.
func TestTitleSlugSkipsHeadingID(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	out := string(Section(SectionConfig{Heading: "Features", Ctx: ctx})) +
		string(Section(SectionConfig{Heading: "Features title", Ctx: ctx}))
	noDuplicateIDs(t, out)
	if !strings.Contains(out, `id="features-title-2"`) {
		t.Errorf("second section's root should be features-title-2:\n%s", out)
	}
	// The other order: "Features" may not take a heading id that
	// "Features title" already owns.
	ctx = render.WithIDScope(context.Background())
	out = string(Section(SectionConfig{Heading: "Features title", Ctx: ctx})) +
		string(Section(SectionConfig{Heading: "Features", Ctx: ctx}))
	noDuplicateIDs(t, out)
	if !strings.Contains(out, `id="features-2"`) {
		t.Errorf("second section's root should be features-2:\n%s", out)
	}
}

// An explicit id wins on the page even when a derived one asked first.
func TestExplicitSectionIDWins(t *testing.T) {
	ids := render.NewRegionBatch(render.WithIDScope(context.Background()), 1)
	ctx := ids.Context(0)
	auto := Section(SectionConfig{Heading: "Features", Ctx: ctx})
	named := Section(SectionConfig{Heading: "Pricing", ID: "features", Ctx: ctx})
	settle := ids.Settle(0)
	a, b := string(settle(auto)), string(settle(named))
	noDuplicateIDs(t, a+b)
	if !strings.Contains(b, `id="features"`) || !strings.Contains(a, `id="features-2"`) {
		t.Errorf("explicit features must stay, derived must move:\n%s\n%s", a, b)
	}
}
