package freeze

import (
	"errors"
	"os/exec"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// ErrGenerateViaBlueprint signals that automated codegen from a frozen Kiln
// world is temporarily unavailable. The legacy entities/*.json → gen/ codegen
// path was removed in favour of the gofastr.yml blueprint; emitting a blueprint
// from a Kiln world is a tracked follow-up (see
// framework/docs/content/agent-notes.md, 2026-06-08).
var ErrGenerateViaBlueprint = errors.New(
	"kiln freeze: automated codegen is pending blueprint support — the snapshot " +
		"under <dir>/ is written; declare the frozen entities in a gofastr.yml " +
		"blueprint and run `gofastr generate --from=gofastr.yml` to produce Go")

// FreezeAndGenerate writes the world's snapshot to dir (entities/*.json +
// world.json) and previously also invoked `gofastr generate` to produce the
// typed package alongside. The pkgPath argument named the relative output
// directory for that generated Go code.
//
// The generate step is currently unavailable: it depended on the removed
// entities/*.json → gen/ codegen path. The snapshot is still written; the
// function then returns ErrGenerateViaBlueprint so callers can graduate the
// frozen world manually via a gofastr.yml blueprint.
func FreezeAndGenerate(w *world.World, dir, _ string) error {
	if err := Freeze(w, dir); err != nil {
		return err
	}
	return ErrGenerateViaBlueprint
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
