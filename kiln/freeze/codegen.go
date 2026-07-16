package freeze

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// ErrGenerateViaBlueprint remains as a source-compatible sentinel for callers
// compiled against the old deferred pipeline. FreezeAndGenerate no longer
// returns it: Kiln now emits and invokes the blueprint directly.
var ErrGenerateViaBlueprint = errors.New("kiln freeze: generate via blueprint")

// FreezeAndGenerate writes gofastr.yml + world.json, then invokes the current
// one-shot generator. pkgPath is an optional relative --out directory.
func FreezeAndGenerate(w *world.World, dir, pkgPath string) error {
	if err := Freeze(w, dir); err != nil {
		return err
	}
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		return fmt.Errorf("freeze: generate: %w", LookForGofastr())
	}
	args := []string{"generate", "--from=gofastr.yml"}
	if pkgPath != "" {
		args = append(args, "--out="+pkgPath)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("freeze: gofastr generate: %w\n%s", err, output)
	}
	return nil
}

// LookForGofastr reports whether the gofastr binary is reachable on PATH.
func LookForGofastr() error {
	if _, err := exec.LookPath("gofastr"); err != nil {
		return errors.New("gofastr binary not on PATH; run `go install ./cmd/gofastr` from the framework root")
	}
	return nil
}
