package initcmd

import (
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The derived generators (generate client, sdk, cli, endpoints) read the
// entity set from entities/*.go, but only from register.go or the
// per-entity files generate app writes. An init scaffold keeps its
// RegisterAll in entities/entities.go, so on a fresh `gofastr init` app
// every one of them failed with "no entities package found in the current
// directory: run from your app root (the directory holding entities/)",
// run from exactly that directory.
func TestInitEntitiesReadByGenerators(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.Mkdir("entities", 0o755); err != nil {
		t.Fatal(err)
	}
	writeEntitiesGo(".")
	decls, err := appgen.PackReadEntities(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 1 || decls[0].Name != "posts" {
		t.Fatalf("generators read %d entities from the init scaffold, want [posts]: %+v", len(decls), decls)
	}
}
