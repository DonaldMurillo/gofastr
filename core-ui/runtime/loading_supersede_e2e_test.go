package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A superseded navigation's finish settled its shown loading entries,
// which drops the park holding the region's original nodes. When the
// newer navigation then failed, the region was left on the loser's
// skeleton, and the originals (input values, listeners, island state)
// were gone (2026-10-27 bug bash R2). ARCHITECTURE.md promises the
// parked nodes come back exactly on failure, abort or a superseded
// navigation.
//
// Two orders: the winner's After timer fires before the loser stands
// down (the winner takes the loser's park over), and after it (the
// loser puts its park back, and the winner parks the originals).
func TestSupersededNavKeepsPark(t *testing.T) {
	for _, c := range []struct {
		name        string
		winnerShows bool
	}{
		{"winner shown first", true},
		{"loser stands down first", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			tpl := `<template data-cui-loading="l:x" data-cui-after="300" data-cui-min="0"><span id="skeleton">SKELETON</span></template>`
			r := newGapRig(t, tpl, "", "/d")
			ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
			warmModule(t, ctx, r)
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.getElementById('main').firstChild.__orig = true`, nil),
			); err != nil {
				t.Fatal(err)
			}
			r.hold("/x3")
			r.hold("/d")
			if err := chromedp.Run(ctx,
				chromedp.Click(`#goX3`, chromedp.ByID),
				chromedp.Poll(`document.getElementById('skeleton') !== null`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Click(`#goD`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			if c.winnerShows {
				_ = chromedp.Run(ctx, chromedp.Sleep(600*time.Millisecond))
			}
			r.release("/x3")
			_ = chromedp.Run(ctx, chromedp.Sleep(600*time.Millisecond))
			r.release("/d")
			var orig, skeleton bool
			if err := chromedp.Run(ctx,
				chromedp.Sleep(600*time.Millisecond),
				chromedp.Evaluate(`document.getElementById('main').firstChild?.__orig === true`, &orig),
				chromedp.Evaluate(`document.getElementById('skeleton') !== null`, &skeleton),
			); err != nil {
				t.Fatal(err)
			}
			if skeleton {
				t.Error("the failed winner left the region on the superseded navigation's skeleton")
			}
			if !orig {
				t.Error("the region's original nodes did not come back after the winner failed: the superseded navigation dropped its park")
			}
		})
	}
}

// TestSweepRestoresUnfilledRegion pins the apply sweep's half of R2: a
// winner whose answer does not fill a region the loser showed loading
// content on settled the loser's entry, dropping its park, so the
// region stayed on the loser's skeleton after the winner landed.
func TestSweepRestoresUnfilledRegion(t *testing.T) {
	tpl := `<template data-cui-loading="l:x#aside" data-cui-after="300" data-cui-min="0"><span id="sk-aside">ASIDE-SKELETON</span></template>`
	r := newGapRig(t, tpl, "", "")
	r.mu.Lock()
	r.noAside = map[string]bool{"/d": true}
	r.mu.Unlock()
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	warmModule(t, ctx, r)
	var before string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('aside').firstChild.__orig = true; document.getElementById('aside').textContent`, &before),
	); err != nil {
		t.Fatal(err)
	}
	r.hold("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Click(`#goX3`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('sk-aside') !== null`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`#goD`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('main').textContent.includes('MAIN-D')`, new(bool), chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	var orig, skeleton bool
	var aside string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('sk-aside') !== null`, &skeleton),
		chromedp.Evaluate(`document.getElementById('aside').firstChild?.__orig === true`, &orig),
		chromedp.Evaluate(`document.getElementById('aside').textContent`, &aside),
	); err != nil {
		t.Fatal(err)
	}
	r.release("/x3")
	if skeleton {
		t.Errorf("the winner's answer left the aside alone, and the aside stayed on the superseded navigation's skeleton (%q)", aside)
	}
	if !orig {
		t.Errorf("the aside's original nodes did not come back (aside %q, was %q)", aside, before)
	}
}
