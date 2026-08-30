package freeze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property family: a world surface that the live preview ENFORCES must not
// be silently dropped from the graduation artifact. The operator tests
// against enforced behavior; the shipped app must carry it (as a stub, a
// blueprint section, or an explicit freeze refusal) — never vanish.
//
// Surface under test: world.Hooks. The live preview registers every world
// hook on the framework HookRegistry (kiln/render/render.go applyHooks),
// so validation and audit hooks gate every write the operator previews.
// Three artifacts promise they survive graduation:
//
//   - framework/docs/content/kiln.md:198-201: "Declarative Kiln hook/route
//     actions remain exact in `world.json`. Where the current blueprint
//     requires a Go function, freeze emits an owned-Go handler stub with a
//     description naming the declarative action" — and entity endpoints,
//     the other not-live-served surface, do exactly that (endpointMaps).
//   - kiln/freeze/freeze.go:7-8 package doc: world.json is a "lossless
//     authoring snapshot, including declarative actions that graduate as
//     owned-Go handler stubs".
//   - cmd/kiln/freeze.go:62-66 prints "hooks: N" in the "wrote to <dir>/"
//     summary, counting them as part of the freeze output.
//
// blueprintMap (kiln/freeze/blueprint.go:79-106) emits no hooks surface at
// all — grep for Hook over the package returns nothing — so a
// before_create validation hook the operator watched reject bad rows in
// the preview simply never exists in the shipped app. No error, no stub,
// no TODO. Only world.json, which the generated app never reads, retains
// it.
func TestFreezeHookNotSilentlyDropped(t *testing.T) {
	w := world.New()
	w.App = world.AppConfig{Name: "forge", Module: "example.com/forge", DBDriver: "sqlite", DBURL: "forge.db"}
	w.Entities["tasks"] = &world.Entity{
		Name:   "tasks",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	}
	const hookID = "tasks_before_create_validate"
	w.Hooks = []*world.Hook{{
		ID: hookID, Entity: "tasks", When: "before_create",
		Action: world.Action{Kind: world.ActionValidate},
	}}

	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatalf("BlueprintYAML: %v", err)
	}
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("core/yaml rejected freeze output: %v\n%s", err, buf)
	}
	hooks := doc.Map["hooks"]
	if hooks == nil || hooks.Kind != coreyaml.List || len(hooks.List) == 0 {
		t.Fatalf("freeze wrote a world with %d live-enforced hook(s) but the graduation blueprint carries no hooks "+
			"surface: the shipped app silently lacks the validation/audit behavior the operator previewed "+
			"(kiln.md: \"freeze emits an owned-Go handler stub\"; the CLI summary counts \"hooks: %d\").\n%s",
			len(w.Hooks), len(w.Hooks), buf)
	}
	found := false
	for _, h := range hooks.List {
		if h == nil || h.Kind != coreyaml.Map {
			continue
		}
		if id := h.Map["id"]; id != nil && id.Value == hookID {
			found = true
		}
		if e, w := h.Map["entity"], h.Map["when"]; e != nil && w != nil && e.Value == "tasks" && w.Value == "before_create" {
			found = true
		}
	}
	if !found {
		t.Errorf("blueprint has a hooks surface but hook %q (entity tasks, before_create) is not in it:\n%s", hookID, buf)
	}

	// Contrast leg: the same freeze writes world.json, which DOES keep the
	// hook — proving the drop is a blueprintMap omission, not a freeze-wide
	// policy. The generated app never reads world.json.
	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	snap, err := os.ReadFile(filepath.Join(dir, "world.json"))
	if err != nil {
		t.Fatalf("read world.json: %v", err)
	}
	if !strings.Contains(string(snap), hookID) {
		t.Errorf("world.json lost the hook too; expected %q in:\n%s", hookID, snap)
	}
}
