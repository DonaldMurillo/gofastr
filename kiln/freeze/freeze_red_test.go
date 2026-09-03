//go:build red

package freeze_test

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: after Freeze, no file in the output tree contains a secret's
// value — the exact property TestFreezeEmitsSecretEnvRefs pins for
// JWTSecret and SeedPassword (freeze_test.go:126-128), extended to the one
// credential-bearing field it missed.
// Surfaces: kiln/freeze/blueprint.go:100-104 (emits `url: <DBURL>` verbatim
// into gofastr.yml) vs blueprint.go:894 envRef (routes JWTSecret and
// SeedPassword to ${ENV} references).
// Finding: a DB DSN with an embedded password is live credential material;
// freeze writes it verbatim into the committed artifact, so a frozen world
// checked into git ships the production DB password.
// Fix direction: route DBURL through envRef (e.g. ${DATABASE_URL}), or strip
// the userinfo component, preserving envRef's empty-value-stays-empty
// semantics.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestFreezeRedOmitsDSNPassword(t *testing.T) {
	w := world.New()
	w.App.Name = "blog"
	w.App.Module = "example.com/blog"
	// Production-shaped driver + DSN with an embedded password.
	w.App.DBDriver = "postgres"
	w.App.DBURL = "postgres://kiln:S3CRET-DB-PASSWORD@db.internal:5432/prod"
	// Satisfy the production-auth gate the same way the pinned test does so
	// the run reaches the blueprint emitter.
	w.App.Auth.Enabled = true
	w.App.Auth.JWTSecret = "SUPER-SECRET-JWT-VALUE" // nosecret: test fixture
	w.Entities["posts"] = &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
	}

	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		buf, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(buf), "S3CRET-DB-PASSWORD") {
			t.Errorf("SECURITY: [disclosure] frozen file %s contains the literal DB password from the DSN — "+
				"blueprint.go emits DBURL verbatim while JWTSecret/SeedPassword get env refs "+
				"(the pinned no-secret-in-tree property, missed for this field)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
