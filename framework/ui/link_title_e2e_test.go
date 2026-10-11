package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A title link reads as the record's name: the text colour, semibold,
// and no underline until hovered. An inline link beside it keeps its
// primary colour and underline.
func TestE2E_LinkTitleReadsAsAName(t *testing.T) {
	body := string(ui.Link(ui.LinkConfig{ID: "title", Href: "/r/1", Text: "INV-1010", Variant: ui.LinkTitle})) +
		string(ui.Link(ui.LinkConfig{ID: "inline", Href: "/r/2", Text: "Read more"}))
	srv := menuTriggerAxeServer(t, body)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var got struct {
		Deco, InlineDeco, Weight, Color, Text, InlineColor, Primary string
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('style[data-cui-style="ui-link"], link[data-cui-style="ui-link"]')`, nil,
			chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(()=>{
const s=id=>getComputedStyle(document.getElementById(id));
const probe=document.createElement('span');document.body.append(probe);
probe.style.color='var(--color-text)';const text=getComputedStyle(probe).color;
probe.style.color='var(--color-primary)';const primary=getComputedStyle(probe).color;
return {Deco:s('title').textDecorationLine, InlineDeco:s('inline').textDecorationLine,
  Weight:s('title').fontWeight, Color:s('title').color, Text:text,
  InlineColor:s('inline').color, Primary:primary};
})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got.Deco != "none" {
		t.Errorf("title link underlined at rest: %q", got.Deco)
	}
	if got.Color != got.Text {
		t.Errorf("title link colour %s, want the text colour %s", got.Color, got.Text)
	}
	if got.Weight != "600" {
		t.Errorf("title link weight %s, want 600", got.Weight)
	}
	if got.InlineDeco != "underline" || got.InlineColor != got.Primary {
		t.Errorf("inline link lost its underline or colour: %q %s (primary %s)", got.InlineDeco, got.InlineColor, got.Primary)
	}
}
