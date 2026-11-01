package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// An Alert with Text and no Title named its dismiss control from the
// empty Title: "Dismiss: ". It falls back to the Text, as a Toast
// falls back to its Body, and refuses to name it after nothing.
func TestAlertDismissNamedWithoutTitle(t *testing.T) {
	island := Island{Endpoint: "/island/alerts", Signal: "alerts"}
	h := string(Alert(AlertProps{Text: "Two apps need attention.",
		DismissHref: "/apps?dismiss=1", Island: island}, nil))
	if !strings.Contains(h, `aria-label="Dismiss: Two apps need attention."`) {
		t.Fatalf("the dismiss control is not named after the Text: %s", h)
	}
	mustRefuse(t, "a Body-only dismissable Alert with no DismissLabel", func() {
		Alert(AlertProps{Body: render.HTML("<p>x</p>"), DismissHref: "/apps?dismiss=1", Island: island}, nil)
	})
	Alert(AlertProps{Body: render.HTML("<p>x</p>"), DismissHref: "/apps?dismiss=1", DismissLabel: "Dismiss the notice", Island: island}, nil)
}
