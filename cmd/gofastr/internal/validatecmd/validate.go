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

// RunValidate implements `gofastr validate <gofastr.yml|dir>`: it runs every
// generate-time check and a full render without writing a file.
func RunValidate(args []string) {
	if cli.HasHelpFlag(args) {
		printValidateHelp()
		return
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		cli.Fail("Usage: gofastr validate <gofastr.yml|dir>")
		cli.Info("Parses the app spec and runs every generate-time validation. Exit 0 = valid.")
		cli.OsExit(2)
		return
	}
	if extra := positionalArgs(args[1:]); len(extra) > 0 {
		cli.Fail("validate takes one app spec, got extra %q", extra)
		cli.Info("Usage: gofastr validate <gofastr.yml|dir>  (run it once per app spec)")
		cli.OsExit(2)
		return
	}
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
	anchor := path
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		anchor = filepath.Dir(path)
	}
	if err := appgen.ResolveAppSpecModule(&bp, anchor); err != nil {
		failAppSpecValidation(path, err)
		cli.OsExit(1)
		return
	}
	if _, err := appgen.RenderAppSpecCode(bp); err != nil {
		failAppSpecValidation(path, err)
		cli.OsExit(1)
		return
	}
	if findings := appspec.LintUnscopedPII(bp); len(findings) > 0 {
		for _, f := range findings {
			cli.Fail("%s: %s", path, f.Message())
		}
		cli.OsExit(1)
		return
	}
	for _, msg := range appgen.CommonLintFindings(bp, anchor, appgen.ModuleCollisionsFirst) {
		cli.Warn("%s: %s", path, msg)
	}
	cli.Success("App spec %s is valid: %d entity(ies), %d screen(s), %d endpoint(s), %d hook(s)",
		path, len(bp.Entities), len(bp.Screens), len(bp.Endpoints), len(bp.Hooks))
}

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

Exit status is 0 for a valid app spec or when --help or -h appears in any
position. A failed check exits 1. Exit status 2 indicates a missing app
spec path, a first argument starting with --, or an extra path or flag.`)
}

func failAppSpecValidation(path string, err error) {
	msg := strings.TrimPrefix(err.Error(), "app spec: ")
	cli.Fail("%s: %s", path, msg)
}
