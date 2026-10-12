package ui_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A chip the headless-collections module adds at runtime wears the
// same pill the SSR chip does: the ui-tag-input sheet keys on what the
// module builds too, not on classes only the server's render carries.
func TestTagInputAddedChipMatchesSSRChip(t *testing.T) {
	th := theme.Default()
	e, ok := registry.Lookup("ui-tag-input")
	if !ok {
		t.Fatal("ui-tag-input is not registered")
	}
	body := `<style>` + th.CSSCustomProperties() + e.CSSFor(th) + `</style>` +
		string(ui.TagInput(ui.TagInputConfig{Name: "tags", Label: "Tags", Values: []string{"go"}}))
	ctx := moduleTestCtx(t, body)
	if !pollJS(ctx, moduleLoaded("headless-collections")) {
		t.Fatal("the tag field marker never loaded headless-collections")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){const f=document.querySelector('[data-hui-tag-input-field]');f.value='css';f.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelectorAll('[data-hui-tag-input-remove]').length === 2`) {
		t.Fatal("Enter did not commit the draft as a chip")
	}
	const look = `(function(i){const c=document.querySelectorAll('li[data-hui-tag-input-remove]')[i];` +
		`const b=c.querySelector('button');const cs=getComputedStyle(c),bs=getComputedStyle(b);` +
		`return [cs.backgroundColor,cs.borderRadius,cs.display,bs.width,bs.borderTopWidth].join('|');})`
	ssr := evalString(ctx, look+`(0)`)
	added := evalString(ctx, look+`(1)`)
	if ssr == "" || added != ssr {
		t.Errorf("the added chip does not wear the SSR chip's style:\n  ssr   %s\n  added %s", ssr, added)
	}
}
