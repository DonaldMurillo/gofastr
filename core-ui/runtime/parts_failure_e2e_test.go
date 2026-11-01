package runtime

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A deferred part whose request fails (a proxy 502, a dropped
// connection) is retried once; a second failure puts its region in the
// error state: data-cui-loadstate="error" and the runtime's phrase in
// place of the loading content, which otherwise stayed for good.
func TestPartFailureRetriesThenErrors(t *testing.T) {
	const msg = "This section could not load."
	for _, tc := range []struct {
		name      string
		failures  int
		preCommit bool // the failures land before the page commits
		want      string
		wantState string
	}{
		{"retry lands", 1, false, "PART-A-ASIDE", ""},
		{"after commit", 2, false, msg, "error"},
		{"before commit", 2, true, msg, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newPartsSite(t, 10000)
			var mu sync.Mutex
			failed := 0
			s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				fail := failed < tc.failures
				if fail {
					failed++
				}
				mu.Unlock()
				if fail {
					http.Error(w, "bad gateway", http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<template data-cui-fill="l:site#aside">PART-A-ASIDE</template>`)
			})
			var release func()
			if tc.preCommit {
				release = s.gatePage(t, "/a")
			} else {
				release = s.gatePart(t, "/a|aside")
			}
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(s.srv.URL+"/"),
				chromedp.WaitVisible(`#goA`, chromedp.ByID),
			); err != nil {
				t.Fatalf("navigate: %v", err)
			}
			jsClick(t, ctx, `#goA`)
			if tc.preCommit {
				deadline := time.Now().Add(10 * time.Second)
				for s.count("part", "/a|aside") < tc.failures && time.Now().Before(deadline) {
					time.Sleep(30 * time.Millisecond)
				}
				time.Sleep(200 * time.Millisecond)
				release()
				waitContains(t, ctx, `#main`, "A-PRIMARY")
			} else {
				waitContains(t, ctx, `#main`, "A-PRIMARY")
				release()
			}
			waitContains(t, ctx, `#aside`, tc.want)
			waitContains(t, ctx, `#rail`, "PART-A-RAIL")
			var state string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('aside').getAttribute('data-cui-loadstate') || ''`, &state)); err != nil {
				t.Fatal(err)
			}
			if state != tc.wantState {
				t.Errorf("aside data-cui-loadstate = %q, want %q", state, tc.wantState)
			}
			if got := readText(t, ctx, `#aside`); strings.Contains(got, "SKELETON") {
				t.Errorf("aside still holds its loading content: %q", got)
			}
			if n := s.count("part", "/a|aside"); n != 2 {
				t.Errorf("aside part requests = %d, want 2 (the request and one retry)", n)
			}
			if tc.wantState == "" {
				return
			}
			// The next navigation that refills the region drops the state.
			jsClick(t, ctx, `#goB`)
			waitContains(t, ctx, `#aside`, "PART-B-ASIDE")
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('aside').getAttribute('data-cui-loadstate') || ''`, &state)); err != nil {
				t.Fatal(err)
			}
			if state != "" {
				t.Errorf("aside data-cui-loadstate = %q after a refill, want none", state)
			}
		})
	}
}
