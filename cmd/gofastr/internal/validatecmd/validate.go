package validatecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// RunValidate implements `gofastr validate <gofastr.yml|dir>`: parse the
// app spec, run every generate-time validation (including the app.module /
// go.mod consistency check and a full render pass), and exit 0/1. Errors are
// written for agents iterating on an app spec: each names the file (and line
// where the parser provides one), what is wrong, and the remedy.
func RunValidate(args []string) {
	if cli.HasHelpFlag(args) {
		printValidateHelp()
		return
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		cli.Fail("Usage: gofastr validate <gofastr.yml|dir>")
		cli.Info("Parses the app spec and runs every generate-time validation. Exit 0 = valid.")
		cli.OsExit(1)
		return
	}
	path := args[0]
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		failAppSpecValidation(path, err)
		cli.OsExit(1)
		return
	}
	// Module consistency is anchored at the app spec's own directory: the
	// app spec conventionally sits at the project root next to go.mod.
	anchor := path
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		anchor = filepath.Dir(path)
	}
	if err := appgen.ResolveAppSpecModule(&bp, anchor); err != nil {
		failAppSpecValidation(path, err)
		cli.OsExit(1)
		return
	}
	// Render without writing: catches codegen-only failures (bad field
	// literals, unsupported defaults) that pure validation does not reach.
	if _, err := appgen.RenderAppSpecCode(bp); err != nil {
		failAppSpecValidation(path, err)
		cli.OsExit(1)
		return
	}
	// Hard rule: per-user (PII-shaped) data must be scoped before auto-CRUD
	// exposure. An error here, a warning from `gofastr generate`.
	if findings := appspec.LintUnscopedPII(bp); len(findings) > 0 {
		for _, f := range findings {
			cli.Fail("%s: %s", path, f.Message())
		}
		cli.OsExit(1)
		return
	}
	// A module path that collides with the framework's own produces code that
	// cannot build anywhere outside this repo. Warned (not failed) for the same
	// reason `generate` warns: the in-repo examples legitimately declare such
	// paths. Surfaced here because validate → fix → generate is the loop the
	// CLI documents, and a finding that only appears at the last step is a
	// finding the loop is designed to skip past.
	for _, f := range appgen.LintModuleCollision(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	// Uniqueness that spans owners on an owner-scoped entity: a warning,
	// since a value unique across all accounts can be deliberate.
	for _, f := range appgen.LintOwnerScopedUnique(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	cli.Success("App spec %s is valid: %d entity(ies), %d screen(s), %d endpoint(s), %d hook(s)",
		path, len(bp.Entities), len(bp.Screens), len(bp.Endpoints), len(bp.Hooks))
}

func printValidateHelp() {
	fmt.Println(`gofastr validate: validate an app spec without writing files

Usage:
  gofastr validate <gofastr.yml|dir>

Checks:
  YAML decoding and unknown keys
  entity, screen, endpoint, and module consistency
  unscoped PII exposure
  a complete in-memory render of generated files

Exit status is 0 when valid and 1 when any check fails.`)
}

// failAppSpecValidation prints a validation failure prefixed with the
// app spec path. Parser/decoder errors already carry "at line N"; semantic
// errors name the entity/field/screen, since the decoded declarations do not
// retain source positions.
func failAppSpecValidation(path string, err error) {
	msg := strings.TrimPrefix(err.Error(), "app spec: ")
	cli.Fail("%s: %s", path, msg)
}
