package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

func TestRepeaterDefaultsSubmitNames(t *testing.T) {
	body := `<form>` + string(Repeater(RepeaterProps{Name: "links", Label: "Links",
		Items: []RepeaterItem{{Fields: []render.HTML{render.Text("")}}},
	}, nil)) + `</form>`
	server := startBehaviorServer(t, body)
	ctx := behaviorPage(t, server)
	var correct bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const form = document.querySelector('form');
		const add = form.querySelector('[data-hui-repeater-action="add"]');
		const remove = form.querySelector('[data-hui-repeater-action="remove"]');
		return new FormData(form, add).get('links_add') === '1' && new FormData(form, remove).get('links_remove') === '0';
	})()`, &correct)); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("default repeater buttons submit no operation")
	}
}
