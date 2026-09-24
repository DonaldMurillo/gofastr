package headless

import (
	"math"
	"strings"
	"testing"
)

// ─── Breadcrumbs ───────────────────────────────────────────────────

func renderCrumbs(p BreadcrumbsProps) string { return string(Breadcrumbs(p, nil)) }

func TestBreadcrumbsRendersTheTrail(t *testing.T) {
	h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{
		{Text: "Home", Href: "/"},
		{Text: "Docs", Href: "/docs/"},
		{Text: "Entities"},
	}})
	for _, want := range []string{
		"<nav ",
		`aria-label="Breadcrumb"`,
		"<ol",
		`aria-current="page"`,
		`href="/"`,
		`href="/docs/"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("breadcrumbs missing %q:\n%s", want, h)
		}
	}
	if n := strings.Count(h, "<a "); n != 2 {
		t.Errorf("trail rendered %d links, want 2 (the current page names itself as text):\n%s", n, h)
	}
	if n := strings.Count(h, `aria-current`); n != 1 {
		t.Errorf("trail carried %d aria-current attributes, want exactly 1:\n%s", n, h)
	}
	// The separator is real markup, hidden from assistive tech: the
	// ordered list already says the order, the slashes say nothing.
	if n := strings.Count(h, `aria-hidden="true"`); n != 2 {
		t.Errorf("trail rendered %d separators, want 2 (between the three steps):\n%s", n, h)
	}
	// The last step must not link to itself.
	if strings.Contains(h, `href="">`) || strings.Count(h, "<li") != 3 {
		t.Errorf("trail shape is wrong:\n%s", h)
	}
}

func TestBreadcrumbsSaysItsLabelThroughStrings(t *testing.T) {
	h := renderCrumbs(BreadcrumbsProps{
		Label: "Fil d’ariane",
		Items: []Breadcrumb{{Text: "Ici"}},
	})
	if !strings.Contains(h, `aria-label="Fil d’ariane"`) {
		t.Errorf("the caller's label never arrived:\n%s", h)
	}
	// An empty Label falls back to the Strings default, so a partial
	// translation cannot unname the landmark.
	h = renderCrumbs(BreadcrumbsProps{
		Strings: &Strings{BreadcrumbsLabel: "Piste de navigation"},
		Items:   []Breadcrumb{{Text: "Ici"}},
	})
	if !strings.Contains(h, `aria-label="Piste de navigation"`) {
		t.Errorf("the translated label never arrived:\n%s", h)
	}
}

func TestBreadcrumbsCurrentEvenWithHref(t *testing.T) {
	h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{
		{Text: "Settings", Href: "/settings"},
		{Text: "Profile", Href: "/settings/profile", Current: true},
	}})
	if !strings.Contains(h, `aria-current="page"`) {
		t.Errorf("Current=true should mark the step even with an Href:\n%s", h)
	}
	if n := strings.Count(h, "<a "); n != 1 {
		t.Errorf("the current step should render as text, want 1 link:\n%s", h)
	}
}

func TestBreadcrumbsDropsDangerousHrefs(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.example.com/x",
		"java\tscript:alert(1)",
	} {
		h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{
			{Text: "Home", Href: "/"},
			{Text: "Evil", Href: href},
			{Text: "End"},
		}})
		if strings.Contains(h, "javascript:") || strings.Contains(h, "vbscript:") ||
			strings.Contains(h, "data:text/html") || strings.Contains(h, "evil.example.com") {
			t.Errorf("dangerous href %q survived:\n%s", href, h)
		}
		if strings.Count(h, "<a ") != 1 {
			t.Errorf("the dangerous step should degrade to plain text for %q:\n%s", href, h)
		}
	}
}

func TestBreadcrumbsScrubsCarriedText(t *testing.T) {
	h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{
		{Text: "Home", Href: "/"},
		{Text: "E\r\nvil<script>"},
	}})
	if strings.ContainsAny(h, "\r\n\x00") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
}

func TestBreadcrumbsSurvivesAHugeLabel(t *testing.T) {
	big := strings.Repeat("é", 10*1024)
	h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{{Text: big, Href: "/"}}})
	if !strings.Contains(h, strings.Repeat("é", 100)) {
		t.Errorf("a 10 kB label should render (scrubbed), not fail:\n%d bytes", len(h))
	}
}

func TestBreadcrumbsRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"no items", func() { Breadcrumbs(BreadcrumbsProps{}, nil) }},
		{"a blank step text", func() {
			Breadcrumbs(BreadcrumbsProps{Items: []Breadcrumb{{Text: "  ", Href: "/"}}}, nil)
		}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s should refuse at render", c.name)
				}
			}()
			c.call()
		}()
	}
}

// ─── Progress ──────────────────────────────────────────────────────

func renderProgress(p ProgressProps) string { return string(Progress(p, nil)) }

func TestProgressDeterminateRendersValueAndMax(t *testing.T) {
	h := renderProgress(ProgressProps{Value: 30, Max: 100, Label: "Upload"})
	for _, want := range []string{
		"<progress", `max="100"`, `value="30"`, `aria-label="Upload"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("progress missing %q:\n%s", want, h)
		}
	}
}

func TestProgressIndeterminateOmitsValue(t *testing.T) {
	h := renderProgress(ProgressProps{Value: -1, Label: "Working"})
	if strings.Contains(h, "value=") {
		t.Errorf("an indeterminate bar must carry no value attribute:\n%s", h)
	}
	if !strings.Contains(h, `max="100"`) {
		t.Errorf("the ceiling should default to 100:\n%s", h)
	}
}

func TestProgressClampsCarriedValues(t *testing.T) {
	// A value past Max is clamped, never rendered past the bar's own
	// end and never a panic: the value can be request-derived.
	h := renderProgress(ProgressProps{Value: 5000, Max: 100, Label: "x"})
	if !strings.Contains(h, `value="100"`) {
		t.Errorf("a value past Max should clamp to Max:\n%s", h)
	}
	// NaN and the infinities are indeterminate: a number that is not a
	// number cannot label progress, and %v would happily print "NaN".
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		h := renderProgress(ProgressProps{Value: v, Label: "x"})
		if strings.Contains(h, "NaN") || strings.Contains(h, "+Inf") || strings.Contains(h, "-Inf") {
			t.Errorf("a non-finite value leaked into the DOM:\n%s", h)
		}
		if strings.Contains(h, "value=") {
			t.Errorf("a non-finite value should render indeterminate:\n%s", h)
		}
	}
	// A carried Max that is nonsense takes the default rather than
	// rendering max="NaN" or max="-5".
	for _, m := range []float64{math.NaN(), math.Inf(1), -5, 0} {
		h := renderProgress(ProgressProps{Value: 1, Max: m, Label: "x"})
		if !strings.Contains(h, `max="100"`) {
			t.Errorf("Max %v should take the 100 default:\n%s", m, h)
		}
	}
}

func TestProgressVisibleLabelIsWiredToTheBar(t *testing.T) {
	h := renderProgress(ProgressProps{Value: 1, Max: 10, Label: "Storage", LabelVisible: true})
	if !strings.Contains(h, `aria-labelledby=`) {
		t.Errorf("a visible label should wire through aria-labelledby:\n%s", h)
	}
	if !strings.Contains(h, ">Storage<") {
		t.Errorf("the label text should render visibly:\n%s", h)
	}
	if strings.Contains(h, `aria-label="Storage"`) {
		t.Errorf("a visibly labelled bar should not also carry aria-label:\n%s", h)
	}
}

func TestProgressScrubsCarriedText(t *testing.T) {
	h := renderProgress(ProgressProps{Value: 1, Label: "Up\r\nload<script>", Description: "1\r\nof 2"})
	if strings.ContainsAny(h, "\r\n") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
}

func TestProgressSurvivesAHugeLabel(t *testing.T) {
	big := strings.Repeat("a", 10*1024)
	h := renderProgress(ProgressProps{Value: 1, Label: big})
	if !strings.Contains(h, strings.Repeat("a", 100)) {
		t.Errorf("a 10 kB label should render (scrubbed), not fail:\n%d bytes", len(h))
	}
}

func TestProgressRefusesANamelessBar(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a Progress with a blank Label should refuse at render — a nameless bar is a stripe a screen reader cannot identify")
		}
	}()
	Progress(ProgressProps{Value: 1, Label: "  "}, nil)
}

func TestProgressRendersNoClassAtNilClasses(t *testing.T) {
	h := renderProgress(ProgressProps{Value: 1, Label: "x"})
	if strings.Contains(h, "class=") {
		t.Errorf("a nil Classes render must carry no class:\n%s", h)
	}
}
