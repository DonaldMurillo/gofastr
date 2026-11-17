package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

// A part answer carries the fill and a seed delta that does NOT repeat
// the route seed the page request already sent (every page seed includes
// the full route.* family, so the part pre-marks them sent; only names
// the fill's own render seeds travel). Streaming sent one island per
// unit and repeated the route seed verbatim — the bug this replaces.
func TestPartAnswerCarriesFillWithoutRepeatedRouteSeed(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	// A session cookie (the page request mints it; the part never
	// does), or the part answers the 409 reset.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	page, err := client.Get(srv.URL + "/projects/billing")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/projects/billing/issues/42", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Part", "l:shell#aside")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	part := string(body)

	if res.StatusCode != 200 {
		t.Fatalf("part status = %d, want 200 (the aside is a deferred outlet of this route)", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("part Cache-Control = %q, want no-store", cc)
	}
	if sc := res.Header.Get("Set-Cookie"); sc != "" {
		t.Errorf("part minted a session (Set-Cookie %q); only the page request mints", sc)
	}
	if !strings.Contains(part, `<template data-cui-fill="l:shell#aside">`) {
		t.Fatalf("part body must be one fill template:\n%s", part)
	}
	if !strings.Contains(part, "Activity") {
		t.Errorf("part body lost the activity fill (the slow fill arrives as its own request):\n%s", part)
	}
	if n := strings.Count(part, `"route.`); n != 0 {
		t.Errorf("part seed carries %d route.* values, want 0 (the page request already sent them)", n)
	}
}

// The page request under X-Gofastr-Defer skips the deferred aside's
// loader: its envelope fill is the loading content, and the real fill
// arrives only through the part above.
func TestDeferredPageRequestShipsLoadingContent(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest("GET", srv.URL+"/projects/billing/issues/42", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Fills", "2")
	req.Header.Set("X-Gofastr-From", "/projects/billing")
	req.Header.Set("X-Gofastr-Defer", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	page := string(body)

	if !strings.Contains(page, `<template data-cui-fill="l:shell#aside">`) {
		t.Fatalf("deferred page answer must carry the aside's fill slot:\n%.600s", page)
	}
	if strings.Contains(page, "reported this issue") {
		t.Errorf("the deferred page request ran the aside's loader; the fill must travel as a part, not with the page")
	}
	if !strings.Contains(page, "Loading activity") {
		t.Errorf("the deferred region must carry its loading content in place:\n%.600s", page)
	}
}
