package freeze_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Pins: world.json stays owner-only (0600) on create AND overwrite — its own
// writer comment says "0600: world.json is the complete IR ... the snapshot is
// the raw world, owner-only".
// Surfaces: kiln/freeze/freeze.go::writeWorldSnapshot
// Finding: writeWorldSnapshot uses os.WriteFile(path, ..., 0o600), whose mode
// argument only applies at CREATE, then relies on fileperm.Restrict — a no-op
// on Unix (internal/fileperm/permissions_other.go). A pre-existing 0644
// world.json (previous freeze under a different umask, a restored checkout, a
// copy) is overwritten with the fresh raw IR but stays world-readable.
// cmd/gofastr pack had the identical hole fixed (pack.go::writeSecretFile
// open→chmod-handle→write, pinned by TestPackOutOwnerOnlyOnOverwrite); freeze
// never got the cutover.
// Fix direction: open the file (or create with O_EXCL-style control), chmod
// the handle to 0600 before writing, mirroring cmd/gofastr's writeSecretFile.
func TestFreezeWorldJSONOwnerOnlyOnOverwrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}

	// World fixture mirroring freeze_security_test.go: enough shape to pass
	// the production-auth gate and reach the snapshot writer. The entity
	// name doubles as the canary proving THIS freeze wrote the file.
	w := world.New()
	w.App.Name = "blog"
	w.App.Module = "example.com/blog"
	w.App.DBDriver = "postgres"
	w.App.DBURL = "postgres://kiln:S3CRET-DB-PASSWORD@db.internal:5432/prod"
	w.App.Auth.Enabled = true
	w.App.Auth.JWTSecret = "SUPER-SECRET-JWT-VALUE" // nosecret: test fixture
	w.Entities["worldpermcanary"] = &world.Entity{
		Name:   "worldpermcanary",
		Fields: []world.Field{{Name: "title", Type: "string"}},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "world.json")
	if err := os.WriteFile(path, []byte("stale snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err) // defeat umask so the precondition is exact
	}

	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("setup broken: Freeze: %v", err)
	}

	// Non-vacuous: the file must carry this freeze's IR, not the stale bytes.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("setup broken: read world.json: %v", err)
	}
	if !strings.Contains(string(body), "worldpermcanary") {
		t.Fatalf("setup broken: world.json does not carry the frozen world — overwrite did not happen: %q", body)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("setup broken: stat world.json: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [freeze-worldjson-mode] world.json is %v after overwrite — writeWorldSnapshot's 0600 only applies at CREATE and fileperm.Restrict is a no-op on Unix, so a pre-existing 0644 world.json keeps the complete raw IR world-readable; pack got the open→chmod-handle→write cutover (TestPackOutOwnerOnlyOnOverwrite), freeze did not", fi.Mode().Perm())
	}
}
