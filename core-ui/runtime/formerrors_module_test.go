package runtime

import (
	"strings"
	"testing"
)

// TestRuntimeModule_FormErrors pins the failure module's shape: the
// marker-less demand module rpc.js loads on a refused form submission,
// the headless hooks it places the envelope through (never a kit
// class: the kit renames its classes without the kernel noticing),
// the toast fallback, and the IIFE structure the other demand modules
// carry.
func TestRuntimeModule_FormErrors(t *testing.T) {
	src, ok := Module("formerrors")
	if !ok {
		t.Fatal("formerrors module not embedded")
	}
	for _, want := range []string{
		`[data-hui-field]`,       // the field group headless.Field renders
		`[data-hui-choice]`,      // a standalone choice's root
		`[data-hui-field-error]`, // the error node: rendered, reserved or live
		`"live"`,                 // the module's own nodes, told apart for clear()
		`"filled"`,               // a rendered node the module wrote into
		"aria-invalid",
		"aria-describedby",
		"role", // role=alert on the message
		"_toastOrFallback",
		"NS._formErrors",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("formerrors module missing %q", want)
		}
	}
	if strings.Contains(src, "innerHTML") {
		t.Error("formerrors must build its markup with DOM calls, never innerHTML")
	}
	if strings.Contains(src, "fui-") {
		t.Error("formerrors names a kit class: the kernel places errors by headless hook only")
	}
	if size := ModuleSize("formerrors"); size > 4000 {
		t.Errorf("formerrors module is %d bytes, budget is 4000", size)
	}
	trimmed := strings.TrimSpace(src)
	if !strings.HasPrefix(trimmed, "(()=>{'use strict'") && !strings.HasPrefix(trimmed, "(() => {") {
		t.Errorf("formerrors module should be an arrow IIFE, got %q", truncate(trimmed, 40))
	}
	if !strings.HasSuffix(trimmed, "})();") {
		t.Error("formerrors module should end with })();")
	}
	// rpc.js only delegates: the rendering lives here.
	rpc, _ := Module("rpc")
	if !strings.Contains(rpc, "loadModule('formerrors')") && !strings.Contains(rpc, `loadModule("formerrors")`) {
		t.Error("rpc.js must demand-load formerrors on a refused form submission")
	}
	if strings.Contains(rpc, "data-hui-field-error") {
		t.Error("rpc.js must not render form errors itself; that is the formerrors module's job")
	}
}
