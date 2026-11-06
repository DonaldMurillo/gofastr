package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func waitLiveReady(ctx context.Context, count int64, timeout time.Duration) error {
	return chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`window.__readyCount >= %d`, count), nil,
		chromedp.WithPollingTimeout(timeout)))
}

func pollLiveReload(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var reloaded bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__sameBuildDocument !== true`, &reloaded)); err != nil {
			return err
		}
		if reloaded {
			return fmt.Errorf("location.reload() was called on same-build-ID reconnect")
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLivereloadProbesFailClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `<!doctype html><script>window.__readyCount = 0;</script>`)
	}))
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.Evaluate(`window.__sameBuildDocument = true`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if err := waitLiveReady(ctx, 2, 100*time.Millisecond); err == nil {
		t.Error("missing reconnect-ready event was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := pollLiveReload(cancelled, 100*time.Millisecond); err == nil {
		t.Error("failed reload-state reads were accepted")
	}
	if err := pollLiveReload(ctx, 0); err != nil {
		t.Fatalf("successful no-reload read failed: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__readyCount = 3; delete window.__sameBuildDocument`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := waitLiveReady(ctx, 3, time.Second); err != nil {
		t.Fatalf("observed ready count was refused: %v", err)
	}
	if err := pollLiveReload(ctx, 0); err == nil {
		t.Error("observed reload was accepted")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__sameBuildDocument = true`, nil),
		chromedp.Reload(),
	); err != nil {
		t.Fatal(err)
	}
	if err := pollLiveReload(ctx, 0); err == nil {
		t.Error("replacement document was accepted as the original page")
	}
}
