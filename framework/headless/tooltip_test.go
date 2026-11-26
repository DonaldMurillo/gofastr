package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The trigger names the pop in aria-describedby next to any description
// it already has, so neither is lost.
func TestTooltipJoinsTheTriggersDescription(t *testing.T) {
	cases := []struct{ name, trigger, want string }{
		{"no description yet", `<button type="button">Save</button>`, `<button type="button" aria-describedby="tip">`},
		{"an existing description", `<button type="button" aria-describedby="help">Save</button>`, `<button type="button" aria-describedby="help tip">`},
		{"already named", `<button aria-describedby="tip">Save</button>`, `<button aria-describedby="tip">`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(Tooltip(TooltipProps{Text: "Saves the draft", ID: "tip", Trigger: render.HTML(c.trigger)}, nil))
			if !strings.Contains(got, c.want) {
				t.Fatalf("got %s\nwant the trigger to open with %s", got, c.want)
			}
		})
	}
}
