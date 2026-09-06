package main

// HTTP-level tests for the example's boundaries: the lobby form, the
// cookie exchange, the room guard, the signaler's authorize gate, and
// the response headers the browser enforces. The chromedp suite
// (browser_test.go) drives the call itself through a real browser.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/rtc"
)

// noRedirectClient reads 303s instead of following them.
var noRedirectClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

// newTestApp builds a fresh app + server. The example holds no
// process-global store (rooms live in the signaler), so one server
// per test is isolation enough. Plugin Init (which mounts the
// signaler's route) runs at Start in production; tests drive the
// router directly, so they fire it by hand.
func newTestApp(t *testing.T) *httptest.Server {
	t.Helper()
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv
}

// joinAs performs the lobby's POST and returns the display-name cookie.
func joinAs(t *testing.T, srv *httptest.Server, name, room string) *http.Cookie {
	t.Helper()
	resp, err := noRedirectClient.PostForm(srv.URL+"/join", url.Values{"name": {name}, "room": {room}})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == callNameCookie {
			return c
		}
	}
	t.Fatal("join set no call_name cookie")
	return nil
}

// The lobby renders the join form with both fields.
func TestLobbyRendersJoinForm(t *testing.T) {
	srv := newTestApp(t)
	page := getPage(t, srv, "/")
	for _, want := range []string{
		`form action="/join"`,
		`id="call-name-input"`,
		`id="call-room-input"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("lobby missing %s", want)
		}
	}
}

// The join exchange sets the HttpOnly cookie and redirects to the
// room; the Secure flag follows the request's TLS signal.
func TestJoinSetsCookieAndRedirects(t *testing.T) {
	srv := newTestApp(t)
	cookie := joinAs(t, srv, "Ann", "e2e")
	if cookie.Value != "Ann" || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookie)
	}

	// Behind TLS the cookie is never sent in clear.
	resp, err := noRedirectClient.PostForm(srv.URL+"/join", url.Values{"name": {"Ann"}, "room": {"e2e"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/room/e2e" {
		t.Fatalf("Location = %q", loc)
	}

	req, _ := http.NewRequest("POST", srv.URL+"/join", strings.NewReader(url.Values{"name": {"Ann"}, "room": {"e2e"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp2, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	var secure *bool
	for _, c := range resp2.Cookies() {
		if c.Name == callNameCookie {
			secure = &c.Secure
		}
	}
	if secure == nil || !*secure {
		t.Fatalf("cookie behind proxy TLS: Secure=%v", secure)
	}
}

// Invalid input mints nothing: every refusal is the same redirect to
// the lobby, so a caller learns nothing about which rooms exist.
func TestJoinValidatesInput(t *testing.T) {
	srv := newTestApp(t)
	cases := []struct{ name, room string }{
		{"", "e2e"},
		{strings.Repeat("a", 41), "e2e"},
		{"bad\x00name", "e2e"},
		{"Ann", ""},
		{"Ann", "E2E"},
		{"Ann", "has space"},
		{"Ann", strings.Repeat("r", 41)},
	}
	for _, c := range cases {
		resp, err := noRedirectClient.PostForm(srv.URL+"/join", url.Values{"name": {c.name}, "room": {c.room}})
		if err != nil {
			t.Fatalf("join %q/%q: %v", c.name, c.room, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
			t.Errorf("join %q/%q = %d %q, want 303 /", c.name, c.room, resp.StatusCode, resp.Header.Get("Location"))
		}
		if len(resp.Cookies()) != 0 {
			t.Errorf("join %q/%q set cookies: %v", c.name, c.room, resp.Cookies())
		}
	}
}

// A cross-site join POST is refused even before validation runs.
func TestCrossSiteJoinRefused(t *testing.T) {
	srv := newTestApp(t)
	req, _ := http.NewRequest("POST", srv.URL+"/join", strings.NewReader(url.Values{"name": {"Ann"}, "room": {"e2e"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site join = %d, want 403", resp.StatusCode)
	}
}

// The room screen refuses without the cookie, and a room name the
// form would never produce answers the same redirect.
func TestRoomRequiresCookieAndValidRoom(t *testing.T) {
	srv := newTestApp(t)
	for _, tc := range []struct{ path, want string }{
		{"/room/e2e", "/"}, // no cookie
		{"/room/E2E", "/"}, // bad room, with cookie
	} {
		req, _ := http.NewRequest("GET", srv.URL+tc.path, nil)
		if tc.path != "/room/e2e" {
			req.AddCookie(&http.Cookie{Name: callNameCookie, Value: "Ann"})
		}
		resp, err := noRedirectClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != tc.want {
			t.Errorf("GET %s = %d %q, want 303 %s", tc.path, resp.StatusCode, resp.Header.Get("Location"), tc.want)
		}
	}
}

// The room page ships the whole page-to-script contract: the config
// carrier, the grid with the local tile, both templates, the chat
// list, and the three controls with Mute waiting for a share.
func TestRoomRendersContract(t *testing.T) {
	srv := newTestApp(t)
	cookie := joinAs(t, srv, "Ann", "e2e")
	req, _ := http.NewRequest("GET", srv.URL+"/room/e2e", nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("room = %d", resp.StatusCode)
	}
	for _, want := range []string{
		`id="call-root"`,
		`data-call-room="e2e"`,
		`data-call-ws="/__gofastr/rtc"`,
		`id="call-grid"`,
		`id="call-local"`,
		`id="call-local-name">Ann<`,
		`<template id="call-tile"`,
		`<template id="call-chat-line"`,
		`id="call-chat"`,
		`id="call-chat-form"`,
		`id="call-share"`,
		`id="call-mute"`,
		`id="call-leave"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("room page missing %s", want)
		}
	}
	mute := openingTag(t, page, "call-mute")
	if !strings.Contains(mute, "disabled") {
		t.Errorf("mute button not disabled at render: %s", mute)
	}
	// Exactly one server-rendered tile (the local one); remote tiles
	// are clones of the template.
	if n := strings.Count(page, "data-call-tile"); n != 1 {
		t.Errorf("room page has %d data-call-tile elements, want 1 (the template)", n)
	}
}

// The signaler's gate runs before any upgrade attempt: no cookie is
// 401, a bad room is 400.
func TestSignalerAuthorizeGate(t *testing.T) {
	srv := newTestApp(t)
	cookie := joinAs(t, srv, "Ann", "e2e")

	resp, err := noRedirectClient.Get(srv.URL + rtc.DefaultPath + "?room=e2e")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signaler without cookie = %d, want 401", resp.StatusCode)
	}

	for _, room := range []string{"", "E2E", "has%20space"} {
		req, _ := http.NewRequest("GET", srv.URL+rtc.DefaultPath+"?room="+room, nil)
		req.AddCookie(cookie)
		resp, err := noRedirectClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("signaler room=%q = %d, want 400", room, resp.StatusCode)
		}
	}
}

// The Permissions-Policy header opens camera and microphone to this
// origin only: getUserMedia works on the room page and is refused by
// the browser everywhere else the app renders.
func TestPermissionsPolicyOpensCameraAndMic(t *testing.T) {
	srv := newTestApp(t)
	cookie := joinAs(t, srv, "Ann", "e2e")
	req, _ := http.NewRequest("GET", srv.URL+"/room/e2e", nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := resp.Header.Get("Permissions-Policy"),
		rtc.PermissionsPolicy(rtc.Camera, rtc.Microphone); got != want {
		t.Fatalf("Permissions-Policy = %q, want %q", got, want)
	}
}

// The room page carries per-user state (the display name) and must
// not enter shared or history caches. The host sends Cache-Control:
// no-store on every rendered page; this pins the contract the example
// relies on rather than adding a header of its own.
func TestRoomPageIsNoStore(t *testing.T) {
	srv := newTestApp(t)
	cookie := joinAs(t, srv, "Ann", "e2e")
	req, _ := http.NewRequest("GET", srv.URL+"/room/e2e", nil)
	req.AddCookie(cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); resp.StatusCode != http.StatusOK || !strings.Contains(cc, "no-store") {
		t.Fatalf("room = %d Cache-Control %q", resp.StatusCode, cc)
	}
}

// ── helpers ──────────────────────────────────────────────────────

// The script asset serves on its own route, hash-versioned by the
// script handler, and answers with the embedded bytes.
func TestScriptAssetServes(t *testing.T) {
	srv := newTestApp(t)
	resp, err := srv.Client().Get(srv.URL + "/__call/app.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("app.js = %d", resp.StatusCode)
	}
	if string(body) != string(appJS) {
		t.Fatalf("app.js served %d bytes, embedded is %d", len(body), len(appJS))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("app.js Content-Type = %q", ct)
	}
}

func getPage(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	return string(body)
}

// openingTag returns the opening tag that carries id="<id>".
func openingTag(t *testing.T, page, id string) string {
	t.Helper()
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("no element id=%q", id)
	}
	start := strings.LastIndex(page[:i], "<")
	end := strings.Index(page[start:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("malformed tag around id=%q", id)
	}
	return page[start : start+end+1]
}
