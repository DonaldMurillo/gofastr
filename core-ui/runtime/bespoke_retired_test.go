package runtime

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// The bespoke-behaviour modules are retired: ConditionalField,
// FileUpload and FileDropzone render their behaviour through the
// headless module's data-hui-when / data-hui-drop hooks (the drop
// forwarding, the chosen-files list and the pick announcement), so
// the old data-fui-scoped modules ran beside their replacement on
// every page that had both hooks. The tracker's rule is "retire in
// the PR where the component moves, never run side by side" — this
// holds the retirement: no src file, no module-table entry, no
// preload row, nothing re-registering the name. The one piece with
// no headless counterpart, the dropzone's image-preview strip, ships
// as framework/ui's own "filedropzone" module instead.
// The stateful-control modules are retired with this change too:
// NumberInput, Slider, RangeSlider, TagInput, Repeater and
// FormRepeater render through headless primitives whose data-hui-*
// hooks the registered headless-controls / headless-collections
// modules bind, so the old data-fui-scoped steppers, mirrors, pairs,
// chip strips and form-repeat interceptors ran beside their
// replacements on every page that had both. searchinput and shortcut
// are the deliberate retention: SearchInput stays a styled wrapper
// until the Batch 3 Combobox decision, and its module with it.
var retiredModuleNames = []string{
	"conditionalfield", "fileupload", "dropzone",
	"numberinput", "slider", "rangeslider", "taginput", "formrepeater",
	"animatedcounter", "backtotop", "banner", "copy", "networkretrybanner",
	"themeswitch", "toasts",
}

func TestBespokeBehaviourModulesAreRetired(t *testing.T) {
	for _, name := range retiredModuleNames {
		if _, ok := Module(name); ok {
			t.Errorf("a module named %s is still served — the headless module owns this behaviour; two modules binding one control is the double-arm the retirement exists to prevent", name)
		}
		for _, listed := range ModuleNames() {
			if listed == name {
				t.Errorf("%s is listed in the module manifest", name)
			}
		}
	}
	// The kernel's marker table (frag/boot.js) and the preload map
	// must not point at them either: a stale row demand-loads a 404.
	boot, err := fs.ReadFile(fragFS, "frag/boot.js")
	if err != nil {
		t.Fatalf("read boot fragment: %v", err)
	}
	for _, name := range retiredModuleNames {
		if strings.Contains(string(boot), "'"+name+"'") {
			t.Errorf("frag/boot.js still demand-loads %s", name)
		}
	}
	for _, m := range demandLoadMarkers {
		switch m.Module {
		case "conditionalfield", "fileupload", "dropzone",
			"numberinput", "slider", "rangeslider", "taginput", "formrepeater",
			"animatedcounter", "backtotop", "banner", "copy", "networkretrybanner",
			"themeswitch", "toasts":
			t.Fatalf("preload still maps %q to a retired module", m.Marker)
		}
	}
	// The two registered UI action adapters are gone with their
	// sources: the headless action contract owns the lifecycle, and a
	// registration left beside it would double-bind every action
	// button.
	for _, gone := range []string{"optimisticaction", "toggleaction"} {
		if _, ok := registry.LookupBehavior(gone); ok {
			t.Errorf("the registered behaviour %q is still live — the headless action module owns the button; delete the adapter's registration with its source", gone)
		}
	}
	// The retention, held as hard as the retirement: SearchInput is a
	// styled wrapper by binding decision, and the shortcut module's
	// callers (ShortcutHint, GlobalSearch, CommandPalette) are Batch
	// 3's. Either name going missing is a silent break, not a cleanup.
	for _, kept := range []string{"searchinput", "shortcut"} {
		if _, ok := Module(kept); !ok {
			t.Errorf("%s is no longer served — it is retained on purpose (SearchInput is a styled wrapper; the shortcut callers are Batch 3's); restore it or change the binding decision with it", kept)
		}
	}
}
