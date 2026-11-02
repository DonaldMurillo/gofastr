package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Words a screen reader speaks that bypassed Strings: the carousel's
// aria-roledescription ("carousel", "slide"; sortable rows route
// theirs through Strings.SortableItemRole) and PaneHost's default
// region names ("Secondary", "Tertiary").
func TestCarouselAndPaneHostWordsGoThroughStrings(t *testing.T) {
	prev := stringsProbe
	stringsProbe = ProbeStrings()
	t.Cleanup(func() { stringsProbe = prev })
	c := string(Carousel(CarouselProps{Label: "Featured", Slides: []CarouselSlide{{Content: render.Text("One")}}}, nil))
	for _, eng := range []string{`aria-roledescription="carousel"`, `aria-roledescription="slide"`} {
		if strings.Contains(c, eng) {
			t.Errorf("Carousel says %s in English under the probe Strings", eng)
		}
	}
	ph := string(PaneHost(PaneHostProps{Primary: render.Text("P"), Secondary: render.Text("S"), Tertiary: render.Text("T")}, nil))
	for _, eng := range []string{`aria-label="Secondary"`, `aria-label="Tertiary"`} {
		if strings.Contains(ph, eng) {
			t.Errorf("PaneHost says %s in English under the probe Strings", eng)
		}
	}
}

// A caller's Strings reaches PaneHost, and a label the caller gave
// still wins over the default name.
func TestPaneHostDefaultNamesFollowStrings(t *testing.T) {
	w := &Strings{PaneSecondary: "Secundario", PaneTertiary: "Terciario"}
	ph := string(PaneHost(PaneHostProps{Primary: render.Text("P"), Secondary: render.Text("S"),
		Tertiary: render.Text("T"), TertiaryLabel: "Cliente", Strings: w}, nil))
	if !strings.Contains(ph, `aria-label="Secundario"`) || !strings.Contains(ph, `aria-label="Cliente"`) {
		t.Fatalf("PaneHost did not name its panes from Strings and the caller's label: %s", ph)
	}
}
