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
	// One app spec per run. A second path was never read, so
	// `validate a.yml b.yml` reported a.yml valid and said nothing of b.yml.
	if extra := positionalArgs(args[1:]); len(extra) > 0 {
		cli.Fail("validate takes one app spec, got extra %q", extra)
		cli.Info("Usage: gofastr validate <gofastr.yml|dir>  (run it once per app spec)")
		cli.OsExit(2)
		return
	}
	// validate reads no flags: `validate gofastr.yml --json` printed text
	// as if --json meant something.
	if extra := flagArgs(args[1:]); len(extra) > 0 {
		cli.Fail("validate takes no flags, got %q", extra)
		cli.Info("Usage: gofastr validate <gofastr.yml|dir>")
		cli.OsExit(2)
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
	// A screen over an entity that needs a session, in an app with no
	// sign-in: the screen draws "Not available" for every visitor.
	for _, f := range appgen.LintSignedOutScreens(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	// A custom_form that POSTs to a screen route: screens answer GET only.
	for _, f := range appgen.LintPostFormsOnScreens(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	// Image or file fields with no file storage wired: uploads answer 400.
	for _, f := range appgen.LintUploadFields(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	// A route under a locale-shaped first segment with locale_prefixes on.
	for _, f := range appgen.LintLocaleRoutes(bp) {
		cli.Warn("%s: %s", path, f.Message())
	}
	cli.Success("App spec %s is valid: %d entity(ies), %d screen(s), %d endpoint(s), %d hook(s)",
		path, len(bp.Entities), len(bp.Screens), len(bp.Endpoints), len(bp.Hooks))
}

// positionalArgs returns the arguments that are not flags.
// flagArgs returns the arguments that start with "-".
func flagArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func positionalArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
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

Exit status is 0 when valid, 1 when any check fails, and 2 for a usage
error such as a second path (validate one app spec per run).`)
}

// failAppSpecValidation prints a validation failure prefixed with the
// app spec path. Parser/decoder errors already carry "at line N"; semantic
// errors name the entity/field/screen, since the decoded declarations do not
// retain source positions.
func failAppSpecValidation(path string, err error) {
	msg := strings.TrimPrefix(err.Error(), "app spec: ")
	cli.Fail("%s: %s", path, msg)
}
