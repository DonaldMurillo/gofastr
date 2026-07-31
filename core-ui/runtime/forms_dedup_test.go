package runtime

import (
	"strings"
	"testing"
)

// TestFormDispatcher_SingleSourceOfTruth pins the single-listener contract.
// Core owns the document submit bridge so a pre-module interaction is caught.
// src/rpc.js owns request encoding and redirect handling, but installs no
// document listener. widgets.js may keep widget-scoped listeners only.
func TestFormDispatcher_SingleSourceOfTruth(t *testing.T) {
	runtimeJS, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	widgetsJS, ok := Module("widgets")
	if !ok {
		t.Fatal("widgets module not embedded")
	}
	rpcJS, ok := Module("rpc")
	if !ok {
		t.Fatal("rpc module not embedded")
	}

	if !strings.Contains(runtimeJS, "data-fui-rpc") {
		t.Error("runtime.js missing data-fui-rpc form-submit branch")
	}
	if strings.Count(runtimeJS, `document.addEventListener('submit'`) != 1 {
		t.Error("runtime.js must install exactly one document-level submit bridge")
	}
	if !strings.Contains(rpcJS, "redirect:'follow'") &&
		!strings.Contains(rpcJS, "redirect: 'follow'") {
		t.Error("rpc module missing Location-follow path")
	}

	if strings.Contains(rpcJS, `document.addEventListener('submit'`) {
		t.Error("rpc module installs a document-level submit listener")
	}

	// widgets.js must NOT install a second document-scope submit
	// handler. The widget-scope one (widgetEl.addEventListener) is
	// allowed; the document-scope one is the duplicate.
	docSubmit := strings.Count(widgetsJS, `document.addEventListener('submit'`)
	if docSubmit != 0 {
		t.Errorf("widgets.js still installs %d document-level submit handler(s) — should be 0 (delegated to runtime.js)", docSubmit)
	}
}
