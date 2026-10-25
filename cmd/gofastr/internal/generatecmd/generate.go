package generatecmd

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func RunGenerate(args []string) {
	if cli.HasHelpFlag(args) && (len(args) == 1 || strings.HasPrefix(args[0], "-")) {
		printGenerateHelp()
		return
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		for _, a := range args {
			if a == "--watch" {
				runGenerateWatch(args)
				return
			}
		}
		appgen.GenerateProject(args)
		return
	}

	name := args[0]
	if g, ok := appgen.LookupGenerator(name); ok {
		g.Run(args[1:])
		return
	}
	switch name {
	case "all":
		appgen.GenerateProject(args[1:])
	case "ts", "typescript":
		cli.Fail("TypeScript codegen has been removed. Use `gofastr generate sdk` for the typed JS/TS client, or gofastr.settings.yml with a project extension for custom frontend artifacts.")
		cli.Info("See framework/docs/content/sdk.md and codegen.md.")
		cli.OsExit(1)
	default:
		cli.Fail("Unknown generator: %s", name)
		cli.Info("Generators: %s", generatorNames())
		cli.OsExit(1)
	}
}

func printGenerateHelp() {
	fmt.Print(`gofastr generate: run code generators

Usage:
  gofastr generate [--config=<gofastr.settings.yml>] [--out=<dir>] [--clean | --no-clean] [--dry-run] [--json] [--watch]
      Run the derived generators and extensions listed in the codegen
      config, then rebuild entities/client when it exists (gofastr build
      does this too)
  gofastr generate <generator> [args]
      Run one generator

`)
	fmt.Print(generatorHelp())
	fmt.Println(`
Run "gofastr docs codegen", "gofastr docs app-spec", "gofastr docs app-cli",
or "gofastr docs sdk" for the complete contracts.`)
}
