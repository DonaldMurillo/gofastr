package runtime

import (
	"strings"
	"testing"
)

// TestFormDispatcher_SingleSourceOfTruth pins the dedup fix. The global
// form-submit dispatcher MUST live in exactly one place — runtime.js.
// widgets.js historically duplicated it, causing drift (one file got
// updated, the other didn't, and behaviour depended on load order).
//
// This test asserts:
//  1. runtime.js installs a global submit handler that handles
//     data-fui-rpc forms and follows Location for native submits.
//  2. widgets.js does NOT install a second global submit handler at
//     document-scope. (Widget-scoped handlers — `widgetEl.addEventListener`
//     — are fine; the rule is one DOCUMENT-level handler.)
func TestFormDispatcher_SingleSourceOfTruth(t *testing.T) {
	runtimeJS, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	widgetsJS, ok := Module("widgets")
	if !ok {
		t.Skip("widgets module not embedded")
	}

	if !strings.Contains(runtimeJS, "data-fui-rpc") {
		t.Error("runtime.js missing data-fui-rpc form-submit branch")
	}
	// Minified spacing has no space after the colon.
	if !strings.Contains(runtimeJS, "redirect:'follow'") &&
		!strings.Contains(runtimeJS, "redirect: 'follow'") {
		t.Error("runtime.js missing Location-follow path")
	}

	// widgets.js must NOT install a second document-scope submit
	// handler. The widget-scope one (widgetEl.addEventListener) is
	// allowed; the document-scope one is the duplicate.
	docSubmit := strings.Count(widgetsJS, `document.addEventListener('submit'`)
	if docSubmit != 0 {
		t.Errorf("widgets.js still installs %d document-level submit handler(s) — should be 0 (delegated to runtime.js)", docSubmit)
	}
}
