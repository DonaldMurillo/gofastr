package runtime

import (
	"io/fs"
	"strings"
	"testing"
)

// The passwordinput module is retired: ui.PasswordInput renders
// through headless.Password and the headless behaviour module owns the
// reveal (data-hui-reveal), so the old data-fui-scoped module ran
// beside its replacement on every page that had both hooks. The
// tracker's rule is "retire in the PR where the component moves, never
// run side by side" — this holds the retirement: no src file, no
// module-table entry, no preload row, nothing re-registering the name.
func TestPasswordInputModuleIsRetired(t *testing.T) {
	if _, ok := Module("passwordinput"); ok {
		t.Fatal("a module named passwordinput is still served — the headless module's reveal owns this behaviour; two modules binding one control is the double-arm the retirement exists to prevent")
	}
	for _, name := range ModuleNames() {
		if name == "passwordinput" {
			t.Fatal("passwordinput is listed in the module manifest")
		}
	}
	// The kernel's marker table (frag/boot.js) and the preload map
	// must not point at it either: a stale row demand-loads a 404.
	boot, err := fs.ReadFile(fragFS, "frag/boot.js")
	if err != nil {
		t.Fatalf("read boot fragment: %v", err)
	}
	if strings.Contains(string(boot), "passwordinput") {
		t.Fatal("frag/boot.js still demand-loads passwordinput")
	}
	for _, m := range demandLoadMarkers {
		if m.Module == "passwordinput" {
			t.Fatalf("preload still maps %q to the retired module", m.Marker)
		}
	}
}
