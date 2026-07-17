package runtime

import (
	"strings"
	"testing"
)

// TestRuntimeModule_SortableList is the source-inspection gate: the
// sortablelist module must reference every new data-fui-* attr, the
// canCross guard, the aria-live announce path, CSS.escape (selector
// safety), and self-register as a loaded module.
func TestRuntimeModule_SortableList(t *testing.T) {
	src, ok := Module("sortablelist")
	if !ok {
		t.Fatal("sortablelist module not embedded")
	}
	for _, want := range []string{
		"data-fui-sortable-group",     // cross-container guard
		"data-fui-sortable-container", // per-column id in payload
		"data-fui-sortable-version",   // optimistic-concurrency token
		"data-fui-sortable-conflict",  // 409 refetch endpoint
		"canCross",                    // group-match guard fn
		"CSS.escape",                  // selector injection safety
		"aria-live",                   // screen-reader announcements
		"announce",                    // the announce() helper
		"loadedModules",               // self-registers as loaded
		"postCross",                   // cross-container commit fn
		"postOrder",                   // same-container commit fn (#84 adds containerField)
		"containerField",              // #84: container field helper (same-container payload)
		"&moved=",                     // moved field in cross payload
		"&container=",                 // container field in payload
		"&version=",                   // version field in commit body
		"is409",                       // 409 conflict detection var
		"conflictMessage",             // #83: bounded 409 problem-detail reader
		"readBounded",                 // #83: hard 4KB body-read safety bound
		"application/json",            // #83: JSON content-type gate
		"finishConflict",              // #83: announce msg-or-fallback + toast
		"fireToast",                   // #83: best-effort framework toast hook
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sortablelist module missing %q", want)
		}
	}
}
