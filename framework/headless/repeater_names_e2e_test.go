package headless

import (
	"maps"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

func TestRepeaterOptionalSubmitNames(t *testing.T) {
	for _, test := range []struct {
		name, add, remove, value string
	}{
		{name: "unnamed"},
		{name: "add only", add: "append"},
		{name: "remove only", remove: "delete"},
		{name: "custom", add: "append", remove: "delete", value: "yes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `<form>` + string(Repeater(RepeaterProps{Name: "links", Label: "Links",
				AddName: test.add, RemoveName: test.remove, AddValue: test.value,
				Items: []RepeaterItem{{Fields: []render.HTML{render.Text("")}}},
			}, nil)) + `</form>`
			server := startBehaviorServer(t, body)
			ctx := behaviorPage(t, server)
			var submitted struct{ Add, Remove map[string]string }
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const form = document.querySelector('form');
				const add = form.querySelector('[data-hui-repeater-action="add"]');
				const remove = form.querySelector('[data-hui-repeater-action="remove"]');
				return {Add:Object.fromEntries(new FormData(form, add)), Remove:Object.fromEntries(new FormData(form, remove))};
			})()`, &submitted)); err != nil {
				t.Fatal(err)
			}
			wantAdd, wantRemove := map[string]string{}, map[string]string{}
			if test.add != "" {
				value := test.value
				if value == "" {
					value = "1"
				}
				wantAdd[test.add] = value
			}
			if test.remove != "" {
				wantRemove[test.remove] = "0"
			}
			if !maps.Equal(submitted.Add, wantAdd) || !maps.Equal(submitted.Remove, wantRemove) {
				t.Fatalf("submit markers = %+v, want add=%v remove=%v", submitted, wantAdd, wantRemove)
			}
		})
	}
}
