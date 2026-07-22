package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsValidDemoID pins the cookie-key shape guard: only 32 lowercase-hex
// chars are accepted, so an attacker-supplied cookie can't become an oversized
// store key.
func TestIsValidDemoID(t *testing.T) {
	valid := []string{
		"0123456789abcdef0123456789abcdef",
		strings.Repeat("a", 32),
		strings.Repeat("0", 32),
	}
	for _, s := range valid {
		if !isValidDemoID(s) {
			t.Errorf("isValidDemoID(%q) = false, want true", s)
		}
	}
	invalid := []string{
		"",
		"tooshort",
		strings.Repeat("a", 31),
		strings.Repeat("a", 33),
		strings.Repeat("A", 32),            // uppercase
		strings.Repeat("g", 32),            // non-hex
		strings.Repeat("x", 1<<20),         // megabyte key — the attack
		"0123456789abcdef0123456789abcde ", // trailing space
	}
	for _, s := range invalid {
		if isValidDemoID(s) {
			t.Errorf("isValidDemoID(len=%d) = true, want false", len(s))
		}
	}
}

// TestReadDemoCookieRejectsOversized: an over-long cookie is treated as absent,
// so it never reaches the store as a key.
func TestReadDemoCookieRejectsOversized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/__site/interactive/counter", nil)
	req.AddCookie(&http.Cookie{Name: demoCookieName, Value: strings.Repeat("a", 100000)})
	if got := readDemoCookie(req); got != "" {
		t.Fatalf("readDemoCookie accepted an oversized value (len %d), want \"\"", len(got))
	}
}

// TestSortableMoveDedupsCards: a move whose order repeats a card id many times
// must not inflate the persisted column — each known card lands at most once.
func TestSortableMoveDedupsCards(t *testing.T) {
	app := setupServer()

	// Move within "todo" (seed cards k1,k2) with a wildly duplicated order.
	body := strings.NewReader("container=todo&order=k1,k1,k1,k1,k1,k2&version=v1")
	moveReq := httptest.NewRequest(http.MethodPost, "/__site/sortable/move", body)
	moveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	moveRec := httptest.NewRecorder()
	app.Router().ServeHTTP(moveRec, moveReq)
	if moveRec.Code != http.StatusNoContent {
		t.Fatalf("move status = %d, want 204", moveRec.Code)
	}

	// Carry the minted session cookie to the conflict read so we observe THIS
	// session's board.
	var cookie *http.Cookie
	for _, c := range moveRec.Result().Cookies() {
		if c.Name == demoCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("move did not set the site-demo cookie")
	}

	confReq := httptest.NewRequest(http.MethodGet, "/__site/sortable/conflict?container=todo", nil)
	confReq.AddCookie(cookie)
	confRec := httptest.NewRecorder()
	app.Router().ServeHTTP(confRec, confReq)
	if confRec.Code != http.StatusOK {
		t.Fatalf("conflict status = %d, want 200", confRec.Code)
	}

	// The todo column holds two real cards (k1, k2); dedup must keep it at two
	// sort keys regardless of the repeated order. Without dedup it would be six.
	got := strings.Count(confRec.Body.String(), "data-fui-sort-key=")
	if got != 2 {
		t.Fatalf("todo column rendered %d sort keys after a duplicated move, want 2 (board inflated)", got)
	}
}

// TestComponentShowcaseRenderNonEmpty guards the llm.md regression: the
// showcase screen's direct Render() (called by core-ui/app/llmmd.go) must
// produce content, not the empty ContextOnly stub — for both a stateless demo
// and a ctx-dispatched stateful one.
func TestComponentShowcaseRenderNonEmpty(t *testing.T) {
	find := func(slug string) componentEntry {
		for _, e := range componentCatalog {
			if e.Slug == slug {
				return e
			}
		}
		t.Fatalf("catalog entry %q not found", slug)
		return componentEntry{}
	}
	for _, slug := range []string{"button", "sortablelist", "optimisticcreate", "optimisticdelete"} {
		s := &ComponentShowcaseScreen{Entry: find(slug)}
		if got := string(s.Render()); strings.TrimSpace(got) == "" {
			t.Errorf("Render() for %q is empty — llm.md would go blank", slug)
		}
	}
}
