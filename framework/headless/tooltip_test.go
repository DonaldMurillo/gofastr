package headless

import (
	"bytes"
	"log/slog"
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

// A trigger with no opening tag cannot carry aria-describedby: the
// tooltip still renders, and a warning names it, once.
func TestTooltipWarnsWhenItsTriggerCannotBeDescribed(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	for range 2 {
		got := string(Tooltip(TooltipProps{Text: "Saves the draft", ID: "tip-text-only", Trigger: render.HTML("Save")}, nil))
		if !strings.Contains(got, `id="tip-text-only"`) || strings.Contains(got, "aria-describedby") {
			t.Fatalf("got %s, want the pop rendered and no aria-describedby", got)
		}
	}
	if n := strings.Count(logs.String(), "tip-text-only"); n != 1 {
		t.Fatalf("the warning naming tip-text-only was logged %d times, want once:\n%s", n, logs.String())
	}
}
