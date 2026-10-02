package main

import (
	"os"
	"strings"
	"testing"
)

// TestHelpNavThreadsTheRequestContext pins the wiring the render-panic
// observer depends on: helpNav must forward the ctx it is GIVEN into
// component.SafeRenderCtx, never substitute context.Background(). With
// the route area's real request ctx threaded through, a panic inside
// the help nav's sidebar reports to the request's renderdiag observer
// (TestHarness failure, 500 in test binaries); with a background
// context the report only reaches the global log and the guard this
// repo builds is silently defeated for this render path.
//
// A behaviour test cannot observe the threading: the sidebar renders no
// ctx-derived output (its labels are config strings), and its panic
// path is not injectable through the public config. The route area's
// closure passing ctx is compiler-checked against dropping the
// parameter; this pins the one regression the compiler cannot see —
// keeping the parameter but ignoring it.
func TestHelpNavThreadsTheRequestContext(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "component.SafeRenderCtx(ctx, ui.Sidebar(helpNavConfig(path)))") {
		t.Fatal("helpNav no longer forwards its ctx into SafeRenderCtx — a render panic in the help nav would miss the request's observer")
	}
}
