package buildcmd

import (
	"fmt"
)

// subcommand_usage.go collects the short --help usage blocks for the
// subcommands whose --help is routed in by main.ownsHelp. Each begins with
// the `Usage: gofastr <cmd>` line that TestSubcommandHelpIsCommandSpecific
// keys on; keep that anchor when editing.

func printBuildUsage() {
	fmt.Println(`Usage: gofastr build [--no-a11y] [--no-embed-check] [--allow-unverified-embeds] [--pkg=<path>] [-o=<output>]

Runs the project-root codegen extension protocol (if gofastr.settings.yml has
a codegen: section), go vet, the static accessibility + .ui.go hydration-sandbox lints,
the embed-surface server-action gate, then go build.

--no-a11y                 skip the accessibility gate (the .ui.go sandbox gate still runs)
--no-embed-check          skip the embed server-action gate
--allow-unverified-embeds keep proven violations fatal but downgrade a surface the analyzer cannot follow
--pkg=<path>              package to compile (default .); e.g. --pkg ./cmd/server
-o=<output>               binary output path (default bin/server); --output= also works`)
}
