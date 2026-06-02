package freeze

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// FreezeAndGenerate writes the world to dir (entities/*.json + world.json)
// and then runs `gofastr generate` to produce the typed package alongside.
// pkgPath is the relative output directory for the generated Go code (e.g.
// "entities" or "gen/entities").
//
// Requires the gofastr binary on PATH — kiln-built apps that want to graduate
// to typed-repo Go usually have the toolchain installed already. When the
// binary isn't found, returns an error so callers can skip gracefully.
func FreezeAndGenerate(w *world.World, dir, pkgPath string) error {
	if err := Freeze(w, dir); err != nil {
		return err
	}
	if pkgPath == "" {
		pkgPath = filepath.Join("gen", "entities")
	}
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		return fmt.Errorf("freeze: gofastr binary not on PATH: %w", err)
	}
	// gofastr generate rejects absolute --out paths (validateOutputDir). Run
	// the command with cwd = dir so we can pass relative paths.
	cmd := exec.Command(bin, "generate",
		"--entities=entities",
		"--out="+pkgPath,
	)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("freeze: gofastr generate failed: %w\n%s", err, string(out))
	}
	return nil
}

// LookForGofastr reports whether the gofastr binary is reachable on PATH.
// Useful for callers that want to soft-detect codegen capability before
// invoking FreezeAndGenerate.
func LookForGofastr() error {
	if _, err := exec.LookPath("gofastr"); err != nil {
		return errors.New("gofastr binary not on PATH; run `go install ./cmd/gofastr` from the framework root")
	}
	return nil
}
