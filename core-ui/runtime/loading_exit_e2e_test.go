package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestLoadingExitSurvivesSupersede(t *testing.T) {
	rig := newGapRig(t, `<template data-cui-loading="l:x" data-cui-after="0"><span>Loading</span></template>`, "", "")
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	warmModule(t, ctx, rig)
	rig.hold("/x3")
	t.Cleanup(func() { rig.release("/x3") })
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.loadModule('loading')`, nil),
		chromedp.Poll(`!!window.__gofastr._navHooks.loading`, nil),
		chromedp.Evaluate(`(() => {
			const loading = window.__gofastr._navHooks.loading;
			const schedule = loading.schedule;
			window.__finished = 0;
			loading.schedule = options => {
				const state = schedule(options), finish = state.finish;
				state.finish = () => { window.__finished++; return finish(); };
				return state;
			};
			window.__gofastr.navigate('/x3');
		})()`, nil),
		chromedp.Poll(`document.querySelector('[data-cui-loadstate="shown"]') !== null`, nil),
	); err != nil {
		t.Fatal(err)
	}
	rig.release("/x3")
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.querySelector('[data-cui-loadstate="exit"]') !== null`, nil),
		chromedp.Evaluate(`window.__gofastr.navigate('/x')`, nil),
		chromedp.Poll(`location.pathname === '/x' && window.__finished >= 2`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("superseded exit did not run both navigation finalizers: %v", err)
	}
}
