package runtime

import (
	"io/fs"
	"strings"
	"testing"
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
func TestBespokeBehaviourModulesAreRetired(t *testing.T) {
	for _, name := range []string{"conditionalfield", "fileupload", "dropzone"} {
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
	for _, name := range []string{"conditionalfield", "fileupload", "dropzone"} {
		if strings.Contains(string(boot), "'"+name+"'") {
			t.Errorf("frag/boot.js still demand-loads %s", name)
		}
	}
	for _, m := range demandLoadMarkers {
		switch m.Module {
		case "conditionalfield", "fileupload", "dropzone":
			t.Fatalf("preload still maps %q to the retired module", m.Marker)
		}
	}
}
