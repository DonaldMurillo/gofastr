package ui

import (
	"context"
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
