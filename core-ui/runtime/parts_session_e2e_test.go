package runtime

// The session-reset retry (2026-09-27 brief): a part whose only problem
// is the session answers 409 with X-Gofastr-Part-Reset: session — the
// page request beside it re-mints, so the runtime waits for the page
// commit and re-requests that part ONCE instead of reloading the
// document. A second session reset, or any other reset reason, reloads
// as before. The rig is parts_e2e_test.go's (the client behaviour keys
// off the header value, so the rig needs no real cookie state).

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// sessionReset answers the session-flavoured 409.
func sessionReset(w http.ResponseWriter) {
	w.Header().Set("X-Gofastr-Part-Reset", "session")
	w.WriteHeader(http.StatusConflict)
}

// TestPartSessionResetRetriesOnce: one retry after the page commit, no
// reload, the part applied; a server that resets the retry too causes
// exactly one reload.
func TestPartSessionResetRetriesOnce(t *testing.T) {
	s := newPartsSite(t, 10000)
	// Gate the page answer so the part's session reset lands BEFORE
	// the commit — the wait-for-commit path.
	releasePage := s.gatePage(t, "/a")
	n := 0
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			sessionReset(w)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<template data-fui-fill="l:site#aside">PART-A-ASIDE-RETRY</template>`)
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Evaluate(`window.__marker = 42`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	// The part resets while the page is in flight; the page then
	// commits and the retry goes out with the fresh session.
	releasePage()
	waitContains(t, ctx, `#main`, "A-PRIMARY")
	waitContains(t, ctx, `#aside`, "PART-A-ASIDE-RETRY")
	time.Sleep(500 * time.Millisecond) // wait out any would-be reload

	if got := s.count("part", "/a|aside"); got != 2 {
		t.Errorf("aside part requests = %d, want exactly 2 (the reset + the one retry)", got)
	}
	if got := s.count("doc", "/a"); got != 0 {
		t.Errorf("document loads of /a = %d, want 0 (a session reset must not reload)", got)
	}
	var marker int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__marker || 0`, &marker)); err != nil {
		t.Fatal(err)
	}
	if marker != 42 {
		t.Error("the window marker is gone — the document reloaded")
	}

	// --- the retry also resets: exactly one reload ----------------------
	s2 := newPartsSite(t, 10000)
	s2.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) { sessionReset(w) })
	s2.setPartResp("/a|rail", func(w http.ResponseWriter, _ *http.Request) { sessionReset(w) })

	ctx2 := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx2,
		chromedp.Navigate(s2.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate (retry-resets rig): %v", err)
	}
	jsClick(t, ctx2, `#goA`)
	// The reload lands the /a DOCUMENT; the second reset stops there.
	waitContains(t, ctx2, `#doc-a`, "DOC")
	time.Sleep(700 * time.Millisecond) // wait out any would-be second reload
	if got := s2.count("doc", "/a"); got != 1 {
		t.Errorf("document loads of /a after a resetting retry = %d, want exactly 1", got)
	}
	if got := s2.count("part", "/a|aside"); got != 2 {
		t.Errorf("aside part requests = %d, want exactly 2 (the reset + the one retry; no loop)", got)
	}
	var path string
	if err := chromedp.Run(ctx2, chromedp.Evaluate(`location.pathname`, &path)); err != nil {
		t.Fatal(err)
	}
	if path != "/a" {
		t.Errorf("URL after the resetting retry = %s, want /a", path)
	}
}

// TestPartSessionResetRespectsSupersede: a newer navigation during the
// retry wait kills the retry — the superseded navigation's parts die
// with it (abort + no commit), so the reset part is re-requested once,
// never twice.
func TestPartSessionResetRespectsSupersede(t *testing.T) {
	s := newPartsSite(t, 10000)
	// /a's page answer stays gated while the part resets (session) and
	// the retry waits for a commit that a newer click must cancel.
	releaseA := s.gatePage(t, "/a")
	s.setPartResp("/a|aside", func(w http.ResponseWriter, _ *http.Request) { sessionReset(w) })

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(s.srv.URL+"/"),
		chromedp.WaitVisible(`#goA`, chromedp.ByID),
		chromedp.Evaluate(`window.__marker = 42`, nil),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	jsClick(t, ctx, `#goA`)
	// The part has answered the session reset by now (its own request
	// is not gated); the page is still in flight.
	waitFor(t, func() bool { return s.count("part", "/a|aside") >= 1 })
	// The newer navigation supersedes A.
	jsClick(t, ctx, `#goB`)
	waitContains(t, ctx, `#main`, "B-PRIMARY")
	// Releasing A's page answer now must change nothing: its epoch is
	// stale, no commit runs, the pending retry never launches.
	releaseA()
	time.Sleep(600 * time.Millisecond)

	if got := s.count("part", "/a|aside"); got != 1 {
		t.Errorf("aside part requests = %d, want exactly 1 (the superseded navigation's retry must not fire)", got)
	}

	// B's own aside part is the region's content now; A's retried fill
	// must never appear in it.
	if got := readText(t, ctx, `#aside`); got != "PART-B-ASIDE" {
		t.Errorf("aside after supersede = %q, want PART-B-ASIDE (the newer navigation's own part; the superseded retry never fired)", got)
	}
	var marker int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__marker || 0`, &marker)); err != nil {
		t.Fatal(err)
	}
	if marker != 42 {
		t.Error("the document reloaded (marker gone)")
	}
	var path string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &path)); err != nil {
		t.Fatal(err)
	}
	if path != "/b" {
		t.Errorf("URL = %s, want /b (the newer navigation owns the URL)", path)
	}
}

// waitFor polls cond until true (bounded), for server-side counters.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}
