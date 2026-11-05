package runtime

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestReturnRefocusesFreshPane(t *testing.T) {
	for _, mode := range []string{"parent", "self"} {
		t.Run(mode, func(t *testing.T) {
			ctx := returnRig(t)
			returnClick(t, ctx, "to-a", `!!document.getElementById('rec-a')`)
			want := "rec-save"
			if mode == "parent" {
				returnClick(t, ctx, "rec-new", `!!document.getElementById('new-pane')`)
				returnClick(t, ctx, "new-save", `!document.getElementById('new-pane') && `+`document.getElementById('rec-count').textContent === 'REC 1'`)
				want = "rec-new"
			} else {
				returnClick(t, ctx, "rec-save", `document.getElementById('rec-count').textContent === 'REC 1'`)
			}
			var focused string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused)); err != nil {
				t.Fatal(err)
			}
			if focused != want {
				t.Fatalf("return focus = %q, want replacement %q", focused, want)
			}
		})
	}
}

func TestHistoryRefocusesFreshPane(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "query", true: "lower"}[nested], func(t *testing.T) {
			_, ctx := interceptOpenRecord(t)
			returnClick(t, ctx, "a-sort", `!!document.getElementById('rec-a-sorted')`)
			move := `history.back(); true`
			if nested {
				returnClick(t, ctx, "sorted-to-rel", `!!document.getElementById('rel-r1')`)
				move = `history.go(-2); true`
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(move, nil)); err != nil {
				t.Fatal(err)
			}
			if !interceptWait(ctx, `!!document.getElementById('rec-a') && !document.getElementById('rel-r1')`) {
				t.Fatal("Back did not re-render the record")
			}
			var focused string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused)); err != nil {
				t.Fatal(err)
			}
			if focused != "rec-note" {
				t.Fatalf("Back focus = %q, want first fresh control rec-note", focused)
			}
		})
	}
}
