package main

// Static-export check for the headless landing routes: the exporter
// enumerates StaticPaths, so both /examples/headless/<theme>/landing pages
// must land in the export under the /gofastr base path, carrying each
// route's theme wrapper class, and the exported app.css must carry the
// option variables — at :root (the floor) and inside every theme scope.
//
// The newsletter POST is a server-backed action: on the export the page is
// static-mode (data-fui-static on <html>), where the runtime answers a
// data-fui-rpc submit with the framework's "needs the Go server" notice
// instead of a dead request. The test pins both halves of that contract:
// the static marker is present, and the form's island wiring is what the
// notice keys on.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticExportWritesHeadlessLanding(t *testing.T) {
	fwApp := newTestApp(t)
	dir := t.TempDir()
	if err := fwApp.ExportStatic(context.Background(), dir, "/gofastr"); err != nil {
		t.Fatalf("ExportStatic: %v", err)
	}

	cases := []struct {
		seg string
		ref string // the theme wrapper class the page must carry
	}{
		{"default", landingRefFramework.Class()},
		{"dense", landingRefDense.Class()},
	}
	for _, c := range cases {
		t.Run(c.seg, func(t *testing.T) {
			page, err := os.ReadFile(filepath.Join(dir, "examples", "headless", c.seg, "landing", "index.html"))
			if err != nil {
				t.Fatalf("exported page missing: %v", err)
			}
			html := string(page)
			if !strings.Contains(html, c.ref) {
				t.Errorf("exported %s page does not carry its theme wrapper class %s", c.seg, c.ref)
			}
			if !strings.Contains(html, `data-fui-static`) {
				t.Error("exported page is not in static mode: no data-fui-static marker, so server-backed actions would fire dead requests")
			}
			if !strings.Contains(html, landingSubscribePath) {
				t.Errorf("exported page lost the newsletter island wiring for %s — the runtime's \"needs the server\" notice keys on it", landingSubscribePath)
			}
			if !strings.Contains(html, `data-fui-rpc`) {
				t.Error("exported page carries no data-fui-rpc marker — without it the static-mode runtime cannot recognize the form as an RPC and show the \"needs the server\" notice")
			}
			// Under --export-base /gofastr every root-absolute URL the
			// page references is rewritten to resolve under the mount.
			if !strings.Contains(html, "/gofastr/__gofastr/app.css") {
				t.Error("exported page does not reference app.css under the /gofastr base path")
			}
		})
	}

	css, err := os.ReadFile(filepath.Join(dir, "__gofastr", "app.css"))
	if err != nil {
		t.Fatalf("exported app.css missing: %v", err)
	}
	sheet := string(css)
	if !strings.Contains(sheet, "--fui-button-radius") || !strings.Contains(sheet, "--fui-density-control-h") {
		t.Error("exported app.css carries no option variables — neither the :root floor nor the scopes compiled")
	}
	for _, c := range cases {
		if !strings.Contains(sheet, "."+c.ref) {
			t.Errorf("exported app.css has no scope block for .%s", c.ref)
		}
	}
	// The dense scope declares its own option values, not the root's: the
	// compact height and the square corner must be scoped inside it.
	denseBlock := scopedCSSBlock(sheet, landingRefDense.Class())
	if denseBlock == "" {
		t.Fatalf("no scope block found for the dense theme in app.css")
	}
	if !strings.Contains(denseBlock, "--fui-density-control-h") || !strings.Contains(denseBlock, "--fui-button-radius") {
		t.Error("the dense scope block redeclares no option variables; nesting would leak the root's values into it")
	}
	// The option-only twins share their route's palette byte for byte:
	// the twin's scope block minus its option lines equals the route's
	// scope block minus its option lines, on both routes. This is the
	// "same palette, different options" fixture's claim, pinned at the
	// stylesheet rather than through one computed colour.
	for _, pair := range []struct{ name, route, twin string }{
		{"default", landingRefFramework.Class(), landingRefFrameworkTight.Class()},
		{"dense", landingRefDense.Class(), landingRefDenseRelaxed.Class()},
	} {
		routeTokens := withoutOptionLines(scopedCSSBlock(sheet, pair.route))
		twinTokens := withoutOptionLines(scopedCSSBlock(sheet, pair.twin))
		if routeTokens == "" || twinTokens == "" {
			t.Fatalf("%s: a scope block is missing for the route or its twin", pair.name)
		}
		if routeTokens != twinTokens {
			t.Errorf("%s: the option-only twin's palette drifted from its route's:\n--- route ---\n%s\n--- twin ---\n%s", pair.name, routeTokens, twinTokens)
		}
	}
}

// withoutOptionLines drops the compiled --fui-* option declarations from
// a scope block so what remains is the palette: tokens and aliases.
func withoutOptionLines(block string) string {
	var keep []string
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "--fui-") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// scopedCSSBlock extracts the `{ … }` body of the first `.fui-theme-<ref>`
// selector block in a stylesheet. A flat brace scan is enough here: the
// theme emitter writes flat blocks (no nested braces).
func scopedCSSBlock(sheet, ref string) string {
	i := strings.Index(sheet, "."+ref)
	if i < 0 {
		return ""
	}
	rest := sheet[i:]
	start := strings.IndexByte(rest, '{')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(rest[start:], '}')
	if end < 0 {
		return ""
	}
	return rest[start+1 : start+end]
}
