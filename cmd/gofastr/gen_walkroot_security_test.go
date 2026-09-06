package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Pins the directory-walk symlink escape in the generator/pack project
// walks, found by the 2026-09-05 red-probe round (round 4); fixed by
// routing registeredScreenRoutes, maxExistingEntityOrder,
// maxExistingScreenOrder, packReadEntities (per-entity + legacy
// register) and packReadPerScreenFiles through os.Root
// (rootReadDirEntries + root.ReadFile + parse-from-bytes), the same
// confinement fileExistsUnder/readFileUnder already had.
// Family: F24 Untrusted project directories and build inputs
// Property: file discovery and reads under the generator/pack project root
// stay under the root even when a leaf entry is a symlink pointing outside
// it — the invariant write_root_symlink_security_test.go pinned for
// fileExistsUnder/missingCallSite (os.Root), extended to the directory
// walks that enumerate *.go and parse each leaf.
// Surfaces: cmd/gofastr/generate.go::registeredScreenRoutes,
// generate.go::maxExistingEntityOrder, generate.go::maxExistingScreenOrder,
// pack.go::packReadPerEntityFiles (via packReadEntities),
// pack.go::packReadPerScreenFiles (same walk shape as the three above).
// Threat: a checkout carrying entities/outsider.go or screen_outside.go
// as a symlink to a file outside the project made `gofastr generate
// --add` / `gofastr pack` parse that outside file and merge its entity
// declaration, registrar order, and site.Register routes into the
// blueprint the CLI writes back. The assertions below observe exactly
// that: routes and orders derived from a file outside writeRoot.
func TestDirWalksRefuseSymlinkedLeaves(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()

	// Real control files under the root: one entity (order 3), one screen
	// registrar (order 3), one mounted route.
	realEntity := `package entities

func init() { registrars = append(registrars, registrar{order: 3, fn: registerReal}) }

func registerReal(app *framework.App) {
	app.Entity("real", framework.EntityConfig{Table: "real"})
}
`
	if err := os.MkdirAll(filepath.Join(root, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "entities", "real.go"), []byte(realEntity), 0o644); err != nil {
		t.Fatal(err)
	}
	realScreen := `package main

func init() { screenRegistrars = append(screenRegistrars, screenRegistrar{order: 3, fn: mountReal}) }

func mountReal(site *Site) { site.Register("/real-screen", nil) }
`
	if err := os.WriteFile(filepath.Join(root, "screen_real.go"), []byte(realScreen), 0o644); err != nil {
		t.Fatal(err)
	}

	// Baseline with only real files present: the walks see exactly these.
	if routes := registeredScreenRoutes(root); !routes["/real-screen"] {
		t.Fatal("control: registeredScreenRoutes missed the real file's route")
	}
	baseEntityOrder := maxExistingEntityOrder(root)
	if baseEntityOrder != 4 {
		t.Fatalf("control: maxExistingEntityOrder = %d, want 4 (real order 3)", baseEntityOrder)
	}
	baseScreenOrder := maxExistingScreenOrder(root)
	if baseScreenOrder != 4 {
		t.Fatalf("control: maxExistingScreenOrder = %d, want 4 (real order 3)", baseScreenOrder)
	}
	baseDecls, err := packReadEntities(root)
	if err != nil {
		t.Fatalf("packReadEntities baseline: %v", err)
	}
	if len(baseDecls) != 1 || baseDecls[0].Name != "real" {
		t.Fatalf("control: packReadEntities baseline = %v, want exactly the real entity", baseDecls)
	}

	// The outside files a hostile checkout symlinks in: a higher entity
	// order, a higher screen order, and a route that exists nowhere under
	// the root.
	outsideEntity := `package entities

func init() { registrars = append(registrars, registrar{order: 41, fn: registerOutsider}) }

func registerOutsider(app *framework.App) {
	app.Entity("outsider", framework.EntityConfig{Table: "outsider"})
}
`
	outsidePath := filepath.Join(outside, "outsider.go")
	if err := os.WriteFile(outsidePath, []byte(outsideEntity), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "entities", "outsider.go")); err != nil {
		t.Fatal(err)
	}
	outsideScreen := `package main

func init() { screenRegistrars = append(screenRegistrars, screenRegistrar{order: 41, fn: mountOutside}) }

func mountOutside(site *Site) { site.Register("/outside-screen", nil) }
`
	outsideScreenPath := filepath.Join(outside, "screen_outside.go")
	if err := os.WriteFile(outsideScreenPath, []byte(outsideScreen), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideScreenPath, filepath.Join(root, "screen_outside.go")); err != nil {
		t.Fatal(err)
	}

	// The property: adding symlinked leaves changes nothing the walks see.
	if routes := registeredScreenRoutes(root); routes["/outside-screen"] {
		t.Error("SECURITY: [walkroot] registeredScreenRoutes parsed a route out of writeRoot through a leaf symlink")
	}
	if got := maxExistingEntityOrder(root); got != baseEntityOrder {
		t.Errorf("SECURITY: [walkroot] maxExistingEntityOrder = %d, want %d: it read registrar order 41 from outside writeRoot through a leaf symlink", got, baseEntityOrder)
	}
	if got := maxExistingScreenOrder(root); got != baseScreenOrder {
		t.Errorf("SECURITY: [walkroot] maxExistingScreenOrder = %d, want %d: it read screen order 41 from outside writeRoot through a leaf symlink", got, baseScreenOrder)
	}
	decls, err := packReadEntities(root)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	for _, d := range decls {
		if d.Name == "outsider" {
			t.Error("SECURITY: [walkroot] packReadEntities merged an entity declaration from outside writeRoot through a leaf symlink")
		}
	}
}
