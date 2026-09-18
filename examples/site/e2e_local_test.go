package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/local"
)

// The framework/local demo at /forms/draft-notes, end to end in a real
// browser: a draft typed into the page is back after a reload, the
// upload action delivers the declared record to the Go handler and the
// response writes a receipt back, the seeded signal survives a
// client-side navigation away and back, and the mirrored preference
// is rendered by the server after the toggle.

func TestE2E_LocalNotes_DraftSurvivesReloadAndUploads(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+localNotesPath),
		chromedp.WaitVisible(`#draft-text`, chromedp.ByID),
		chromedp.SendKeys(`#draft-title`, "Team notes", chromedp.ByID),
		chromedp.SendKeys(`#draft-text`, "Garchomp leads, Rotom-Wash covers water.", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	// The page script debounces the save; wait for the record.
	if !e2ePollTrue(ctx, `window.__gofastr.loadModule('local-store').then(() => window.__gofastr.localStore('site').collection('drafts').get('current')).then(d => !!d && d.title === 'Team notes')`) {
		t.Fatal("the typed draft never reached drafts:current")
	}

	// Reload: the draft is back in the fields, from IndexedDB.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+localNotesPath),
		chromedp.WaitVisible(`#draft-text`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve(document.getElementById('draft-text').value.indexOf('Garchomp') >= 0 && document.getElementById('draft-title').value === 'Team notes')`) {
		t.Fatal("after reload the draft fields are empty — the browser did not keep the draft")
	}

	// Upload: the Go handler reads the declared record and answers.
	if err := chromedp.Run(ctx, chromedp.Click(`#draft-form button[type=submit]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve((document.getElementById('upload-result').textContent || '').indexOf('received "Team notes"') >= 0)`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Text(`#upload-result`, &got, chromedp.ByID))
		t.Fatalf("upload result = %q — the handler did not read the draft", got)
	}
	// The response wrote a receipt back into the browser; the page
	// script lists it.
	if !e2ePollTrue(ctx, `Promise.resolve(document.querySelectorAll('#receipts li').length >= 1 && document.querySelector('#receipts li').textContent.indexOf('Team notes') >= 0)`) {
		t.Fatal("the receipt the response pushed never showed up")
	}
}

func TestE2E_LocalNotes_SeededSignalSurvivesNavigation(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+localNotesPath),
		chromedp.WaitVisible(`#say-goodbye`, chromedp.ByID),
		chromedp.Click(`#say-goodbye`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve(document.getElementById('scratch-value').textContent === 'goodbye')`) {
		t.Fatal("the signal-set button did not update the seeded element")
	}
	if !e2ePollTrue(ctx, `window.__gofastr.loadModule('local-store').then(() => window.__gofastr.localStore('site').collection('scratch').get('last')).then(v => v === 'goodbye')`) {
		t.Fatal("the seeded signal's change never reached scratch:last")
	}
	// Away through the router, then back: the restored page shows the record.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('/forms/wizard')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve(location.pathname === '/forms/wizard')`) {
		t.Fatal("navigation to /forms/wizard never happened")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve(location.pathname === '/forms/draft-notes' && !!document.getElementById('scratch-value') && document.getElementById('scratch-value').textContent === 'goodbye')`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Text(`#scratch-value`, &got, chromedp.ByID))
		t.Fatalf("after back-navigation the seeded element shows %q, want goodbye", got)
	}
	// A hard reload too: SSR paints the default, the record wins.
	if err := chromedp.Run(ctx, chromedp.Navigate(base+localNotesPath), chromedp.WaitVisible(`#scratch-value`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !e2ePollTrue(ctx, `Promise.resolve(document.getElementById('scratch-value').textContent === 'goodbye')`) {
		t.Fatal("after reload the seeded element did not restore the record")
	}
}

func TestE2E_LocalNotes_MirroredPreferenceRendersOnTheServer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)
	var tag, source string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+localNotesPath),
		chromedp.WaitVisible(`#toggle-view`, chromedp.ByID),
		chromedp.Text(`#view-tag`, &tag, chromedp.ByID),
		chromedp.Text(`#view-source`, &source, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tag, "Comfortable") || !strings.Contains(source, "server default") {
		t.Fatalf("first render: tag %q source %q", tag, source)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#toggle-view`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	// The toggle writes the record (and its cookie) and navigates
	// client-side; the SERVER renders the new value from the cookie.
	if !e2ePollTrue(ctx, `Promise.resolve((document.getElementById('view-tag') || {}).textContent === 'Compact view' && (document.getElementById('view-source') || {textContent: ''}).textContent.indexOf('mirrored cookie') >= 0)`) {
		_ = chromedp.Run(ctx, chromedp.Text(`#view-tag`, &tag, chromedp.ByID), chromedp.Text(`#view-source`, &source, chromedp.ByID))
		t.Fatalf("after toggle: tag %q source %q — the server did not render the mirrored preference", tag, source)
	}
	var cookie string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &cookie)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cookie, "gofastr.local.site.notes-prefs.view=") {
		t.Fatalf("no mirror cookie: %q", cookie)
	}
}

// e2ePollTrue evaluates js (a promise of a boolean) until it resolves
// true or the budget runs out.
func e2ePollTrue(ctx context.Context, js string) bool {
	for range 80 {
		var v bool
		err := chromedp.Run(ctx, chromedp.Evaluate(js, &v, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
		if err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// The draft-notes handler acts on an upload and on nothing else. Reading
// local.Source as a bare boolean (src.Found()) accepts a mirror cookie,
// a value any script on the origin writes and any client forges, and
// the team-builder example (github.com/AlexCiccolella125/gofastr-team-builder)
// already requires SourceUpload. Two examples teaching two habits is how
// the weaker one gets copied.
func TestLocalNotesActsOnlyOnAnUpload(t *testing.T) {
	if !actOnDraft(local.SourceUpload) {
		t.Fatal("an uploaded draft must be acted on")
	}
	for _, src := range []local.Source{local.SourceMirror, local.SourceNone} {
		if actOnDraft(src) {
			t.Fatalf("a %q record must not be acted on: it is a client hint, not a declared upload", src)
		}
	}
}

// The site's logout clears the store. battery/auth cannot: it does not
// know the app's stores, and a mirror cookie lives a year, so without
// this line the next user's first paint renders the previous user's
// preferences. The redirect is a full navigation, so the clear rides
// ClearOnNextLoad, not the response header rpc.js reads.
func TestLocalNotesLogoutPlantsTheClearBit(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, localNotesLogoutPath, nil)
	localNotesLogout(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "gofastr.local.clear.site" && c.Value == "1" && c.Path == "/" {
			return
		}
	}
	t.Fatalf("no gofastr.local.clear.site cookie on the logout response: %v", rec.Header()["Set-Cookie"])
}
