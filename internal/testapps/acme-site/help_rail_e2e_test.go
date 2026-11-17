package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestHelpRailKeepsSiblingContent(t *testing.T) {
	base := acmeServe(t)
	browser := acmeBrowser(t, 1280, 860)
	ctx, cancel := context.WithTimeout(browser, time.Minute)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/help/projects")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!!document.querySelector('[data-cui-outlet$="#toc"] .fui-toc')`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-cui-scope="helpdocs"] > .toc').appendChild(document.createElement('div'))`, nil)); err != nil {
		t.Fatal(err)
	}
	var screenshot []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 90)); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("GOFASTR_TEST_SCREENSHOT_DIR"); directory != "" {
		if err := os.WriteFile(filepath.Join(directory, "help-rail-siblings.png"), screenshot, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if display, columns := tocState(t, ctx); display == "none" || columns != 3 {
		t.Fatalf("filled rail with empty sibling: display=%q, columns=%d; want visible with three columns", display, columns)
	}
	for _, contents := range []string{"", "<div></div>"} {
		var columns int
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const rail = document.querySelector('[data-cui-scope="helpdocs"] > .toc');
			rail.innerHTML = `+"`"+contents+"`"+`;
			return getComputedStyle(rail.parentElement).gridTemplateColumns.split(' ').length;
		})()`, &columns)); err != nil {
			t.Fatal(err)
		}
		if columns != 2 {
			t.Errorf("empty rail %q retains %d columns; want two", contents, columns)
		}
	}
}
