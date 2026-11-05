package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestFilterChipNoScriptNoopSupportsGETAndPOST(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			response := serve(t, method, "/__site/toggle/noop")
			if response.Code != http.StatusNoContent {
				t.Fatalf("%s noop status = %d, want 204", method, response.Code)
			}
			if response.Body.Len() != 0 || response.Header().Get("Location") != "" {
				t.Fatalf("%s noop must not replace or redirect the current page: body=%q, location=%q", method, response.Body.String(), response.Header().Get("Location"))
			}
		})
	}
}

func TestTagDemoDismisses(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tag"), pageReady(),
		chromedp.Click(`a[aria-label="Remove beta"]`),
		chromedp.Poll(`!document.querySelector('a[aria-label="Remove beta"]') && document.querySelectorAll('main').length===1 && document.querySelectorAll('[data-cui-signal="tag-demo"]').length===1`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/components/tag?dismissed=beta"), pageReady(),
		chromedp.Poll(`!document.querySelector('a[aria-label="Remove beta"]')`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
		t.Fatal(err)
	}
}

func TestFilterChipDemoPostsSucceed(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	responses := make(chan int, 4)
	chromedp.ListenTarget(ctx, func(event any) {
		if response, ok := event.(*network.EventResponseReceived); ok && response.Type == network.ResourceTypeFetch &&
			(strings.Contains(response.Response.URL, "/components/filterchipbar?") || strings.Contains(response.Response.URL, "/__site/toggle/noop")) {
			responses <- int(response.Response.Status)
		}
	})
	if err := chromedp.Run(ctx, network.Enable(), chromedp.Navigate(base+"/components/filterchipbar"), pageReady()); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{`.fui-filter-bar a[aria-label]:not(.fui-filter-bar__clear)`, `.fui-filter-bar__clear`} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector(`+fmt.Sprintf("%q", selector)+`).click()`, nil)); err != nil {
			t.Fatalf("%s: %v", selector, err)
		}
		select {
		case status := <-responses:
			if status < 200 || status >= 300 {
				t.Fatalf("chip control answered %d", status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("chip control sent no request")
		}
	}
}
