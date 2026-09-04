package freeze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: no file in the frozen output tree carries a secret's value —
// for the app's credentials (JWTSecret, SeedPassword) and for a DB DSN
// with an embedded password alike; a credentialed DSN is a credential,
// so freeze emits a ${DATABASE_URL} reference instead of the value.
func TestFreezeOmitsDSNPassword(t *testing.T) {
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
			t.Errorf("frozen file %s contains the literal DB password from the DSN — "+
				"both gofastr.yml and world.json must carry a ${DATABASE_URL} reference (or no credential) instead", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// Property: a DSN without credentials (a SQLite file path) stays verbatim
// in the frozen blueprint, so local frozen apps keep booting; only
// credential-bearing DSNs become env references.
func TestFreezeKeepsCredentialFreeDSNVerbatim(t *testing.T) {
	w := world.New()
	w.App.Name = "blog"
	w.App.Module = "example.com/blog"
	w.App.DBDriver = "sqlite"
	w.App.DBURL = "file:blog.db"
	w.App.Auth.DevMode = true
	w.Entities["posts"] = &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
	}

	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatalf("BlueprintYAML: %v", err)
	}
	if !strings.Contains(string(buf), "file:blog.db") {
		t.Errorf("credential-free DSN was substituted away:\n%s", buf)
	}
}
