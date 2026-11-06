package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestRuntimeToastUsesRoleOnly(t *testing.T) {
	for _, template := range []bool{false, true} {
		name, body := "bare", string(ToastStack(ToastStackProps{Label: "Updates"}, nil))
		if template {
			name, body = "template", toastStackWithTemplate()
		}
		t.Run(name, func(t *testing.T) {
			ctx := behaviorPage(t, startBehaviorServer(t, body))
			if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
				t.Fatal("feedback did not load")
			}
			var correct bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`['info', 'success', 'warning', 'danger'].every(variant => {
				const id = window.__gofastr.toast({variant, title: variant});
				const item = document.querySelector('[data-hui-toast-id="' + id + '"]');
				const row = item.querySelector('[data-hui-toast]') || item;
				return row.getAttribute('role') === (['warning', 'danger'].includes(variant) ? 'alert' : 'status') && !row.hasAttribute('aria-live');
			})`, &correct)); err != nil {
				t.Fatal(err)
			}
			if !correct {
				t.Fatal("runtime toast duplicates its role's live semantics")
			}
		})
	}
}

func TestSortableLiveUsesRoleOnly(t *testing.T) {
	browser, _ := sortableServer(t, sortablePage(SortableItem{Key: "a", Label: "Alpha"}, SortableItem{Key: "b", Label: "Beta"}), false)
	ctx := behaviorPage(t, browser)
	if !pollTrue(ctx, sortableLoaded) {
		t.Fatal("sortable behavior did not load")
	}
	var correct bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-sort-key="a"]').focus()`, nil),
		sortKey(" "),
		chromedp.Evaluate(`(() => { const live = document.getElementById('hui-sortable-live'); return live?.getAttribute('role') === 'status' && !live.hasAttribute('aria-live'); })()`, &correct),
	); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("sortable live region duplicates its role's live semantics")
	}
}
