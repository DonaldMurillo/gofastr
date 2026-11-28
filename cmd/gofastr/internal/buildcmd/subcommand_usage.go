package buildcmd

import (
	"fmt"
)

func printBuildUsage() {
	fmt.Println(`Usage: gofastr build [--no-generate] [--no-a11y] [--no-embed-check] [--no-contracts] [--allow-unverified-embeds] [--pkg=<path>] [-o=<output>]

Run from the project root. Build runs codegen for a codegen: section,
an existing entities/client, or app endpoints. It then runs go vet,
the accessibility and .ui.go hydration-sandbox lints, the embed-surface
server-action gate, the contract gate (gofastr verify), and go build.

--no-generate             skip the codegen step
--no-a11y                 skip the accessibility gate (the .ui.go sandbox gate still runs)
--no-embed-check          skip the embed server-action gate
--no-contracts            skip the contract gate
--allow-unverified-embeds keep proven violations fatal but downgrade a surface the analyzer cannot follow
--pkg=<path>              package to compile (default .); e.g. --pkg ./cmd/server
-o=<output>               binary output path (default bin/server); --output=<output> also works`)
}
