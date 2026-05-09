package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestBrowserSmokeServer(t *testing.T) {
	ds := setupHost()
	srv := httptest.NewServer(ds)
	defer srv.Close()
	t.Log("Server URL:", srv.URL)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 15*time.Second)
	defer timeoutCancel()

	var bodyText string
	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Sleep(2*time.Second),
		chromedp.Text("body", &bodyText, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	t.Log("Body text length:", len(bodyText))
	if len(bodyText) == 0 {
		t.Error("body is empty")
	}
}
