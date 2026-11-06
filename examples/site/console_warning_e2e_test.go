package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/chromedp"

	uiruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

func TestConsoleGateCatchesModuleWarnings(t *testing.T) {
	javascript, err := uiruntime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/__gofastr/runtime.js" {
			writer.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(writer, javascript)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/__gofastr/runtime/") {
			writer.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(writer, `window.__unregisteredModuleRan = true;`)
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		fmt.Fprint(writer, `<!doctype html><html><head></head><body>
<button id="invoke" data-cui-rpc="/action">Invoke</button>
<script>console.warn('vendor advisory');const warn = console.warn;console.warn = (...args) => { window.__runtimeWarningSeen = true;warn(...args); };</script>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	}))
	t.Cleanup(server.Close)
	ctx := siteBrowserCtx(t)
	sink := &consoleErrSink{}
	sink.listen(ctx)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.Click(`#invoke`, chromedp.ByQuery),
		chromedp.Poll(`window.__unregisteredModuleRan && window.__runtimeWarningSeen`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Sleep(50*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	entries := sink.errors()
	if len(entries) != 1 || !strings.Contains(entries[0], "RPC module unavailable") {
		t.Fatalf("module registration failure escaped the console gate: %v", entries)
	}
}

func TestConsoleGateFiltersLogWarnings(t *testing.T) {
	sink := &consoleErrSink{}
	for _, entry := range []cdplog.Entry{
		{Level: cdplog.LevelWarning, Text: "vendor advisory"},
		{Level: cdplog.LevelInfo, Text: "[gofastr] ordinary progress"},
		{Level: cdplog.LevelWarning, Text: "[gofastr] module unavailable"},
		{Level: cdplog.LevelError, Text: "unrelated error"},
	} {
		sink.record(&cdplog.EventEntryAdded{Entry: &entry})
	}
	if entries := sink.errors(); len(entries) != 2 || !strings.Contains(entries[0], "module unavailable") || !strings.Contains(entries[1], "unrelated error") {
		t.Fatalf("log ownership filter: %v", entries)
	}
}
