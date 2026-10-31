package freeze

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// generateWaitDelay bounds cmd.Wait once the generator has exited or ctx
// is done.
const generateWaitDelay = 2 * time.Second

// FreezeAndGenerate writes gofastr.yml + world.json, then invokes the current
// one-shot generator under ctx. pkgPath is an optional relative --out
// directory.
func FreezeAndGenerate(ctx context.Context, w *world.World, dir, pkgPath string) error {
	if err := Freeze(w, dir); err != nil {
		return err
	}
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		return fmt.Errorf("freeze: generate: %w", LookForGofastr())
	}
	args := []string{"generate", "app", "gofastr.yml"}
	if pkgPath != "" {
		args = append(args, "--out="+pkgPath)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	// A generator that exits while a child it started still holds the
	// output pipe would otherwise wedge CombinedOutput until that child
	// exits too.
	cmd.WaitDelay = generateWaitDelay
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
