package uihost

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// First load paints from the document and its stylesheets alone. The
// one script allowed to block is the color-scheme bootstrap in <head>:
// it sets data-color-scheme before CSS parses, so dark mode paints
// without a flash. Every other script the host emits is deferred, and
// runtime-module preloads ask for low priority, so hydration never
// competes with first paint for the wire or the main thread.

var firstLoadScriptRe = regexp.MustCompile(`<script\b[^>]*>`)

func TestOnlyColorSchemeScriptBlocks(t *testing.T) {
	ds := newTestUIHost()
	if err := ds.RegisterExternalScript("/plug.js"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	page := w.Body.String()
	headEnd := strings.Index(page, "</head>")
	if headEnd < 0 {
		t.Fatal("no </head> in the page")
	}

	blockingInHead := 0
	srcTags := 0
	for _, loc := range firstLoadScriptRe.FindAllStringIndex(page, -1) {
		tag := page[loc[0]:loc[1]]
		if !strings.Contains(tag, " src=") {
			continue // inline data or code block, not a fetch
		}
		srcTags++
		deferred := strings.Contains(tag, " defer") || strings.Contains(tag, " async")
		if strings.Contains(tag, "/__gofastr/color-scheme.js") {
			if loc[0] > headEnd {
				t.Errorf("color-scheme bootstrap left <head>: %s", tag)
			}
			continue
		}
		if !deferred {
			t.Errorf("script blocks the parser: %s", tag)
			if loc[0] < headEnd {
				blockingInHead++
			}
		}
	}
	if srcTags < 3 {
		t.Fatalf("premise: found %d script tags, want runtime, manifest and the extra script", srcTags)
	}
	if blockingInHead > 0 {
		t.Errorf("%d blocking scripts in <head> besides the color-scheme bootstrap", blockingInHead)
	}
}

func TestModulePreloadsAreLowPriority(t *testing.T) {
	links := runtimeModulePreloadLinks(`<div data-cui-reveal></div><div data-cui-poll="5s"></div>`)
	n := strings.Count(links, `rel="preload"`)
	if n == 0 {
		t.Fatal("premise: no preload links for a page with module markers")
	}
	if got := strings.Count(links, `fetchpriority="low"`); got != n {
		t.Errorf("%d of %d module preloads ask for low priority:\n%s", got, n, links)
	}
}

// The host's deferred scripts close <head>, so they run before any
// deferred script in the page's own markup: page code that reaches for
// window.__gofastr finds it, as it did when runtime.js ran in step.
func TestHostScriptsCloseHead(t *testing.T) {
	ds := newTestUIHost()
	if err := ds.RegisterExternalScript("/plug.js"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	page := w.Body.String()
	headEnd := strings.Index(page, "</head>")
	bodyStart := strings.Index(page, "<body")
	if headEnd < 0 || bodyStart < 0 {
		t.Fatal("premise: no </head> or <body> in the page")
	}
	for _, src := range []string{"/__gofastr/runtime.js", "/plug.js"} {
		i := strings.Index(page, `src="`+src)
		if i < 0 {
			t.Fatalf("premise: no %s script", src)
		}
		if i > headEnd {
			t.Errorf("%s is emitted after </head>; page scripts would run before it", src)
		}
	}
	for _, tag := range firstLoadScriptRe.FindAllString(page[bodyStart:], -1) {
		if strings.Contains(tag, " src=") {
			t.Errorf("a script tag is left in <body>: %s", tag)
		}
	}
}
