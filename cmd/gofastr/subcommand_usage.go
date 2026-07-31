package main

import "fmt"

// subcommand_usage.go collects the short --help usage blocks for the
// subcommands whose --help is routed in by main.ownsHelp. Each begins with
// the `Usage: gofastr <cmd>` line that TestSubcommandHelpIsCommandSpecific
// keys on; keep that anchor when editing.

func printBuildUsage() {
	fmt.Println(`Usage: gofastr build [--no-a11y] [--no-embed-check] [--allow-unverified-embeds] [--pkg=<path>] [-o=<output>]

Runs the project-root codegen extension protocol (if gofastr.codegen.yml is
present), go vet, the static accessibility + .ui.go hydration-sandbox lints,
the embed-surface server-action gate, then go build.

--no-a11y                 skip the accessibility gate (the .ui.go sandbox gate still runs)
--no-embed-check          skip the embed server-action gate
--allow-unverified-embeds keep proven violations fatal but downgrade a surface the analyzer cannot follow
--pkg=<path>              package to compile (default .); e.g. --pkg ./cmd/server
-o=<output>               binary output path (default bin/server); --output= also works`)
}

func printDevUsage() {
	fmt.Println(`Usage: gofastr dev [--addr=<host:port>] [--dir=<path>] [--pkg=<path>] [--no-a11y] [-p <port>]

Builds and runs the server, rebuilding and hot-reloading on file changes.
GOFASTR_DEV=1 is injected so the app wires livereload.

--addr=<host:port>  listen address (default localhost:8080); -p <port> is a short form
--dir=<path>        project root: watch root and server cwd (default .)
--pkg=<path>        package to build, relative to --dir (default .)
--no-a11y           skip the accessibility gate (.ui.go sandbox gate still runs)`)
}

func printMigrateUsage() {
	fmt.Println(`Usage: gofastr migrate [up|down|status|generate|force] [flags]

Run database migrations. The subcommand defaults to 'up'.

  up       apply pending migrations (default)
  down N   roll back the last N migrations (default 1)
  status   show applied/pending migrations
  generate <name>  emit a reviewable migration from a blueprint (--from=<yml>)
  force <V>        mark version V applied without running its SQL (--not-applied removes it)

Common flags: --db-url=<path> --db=<sqlite3|postgres|mysql> --group=<name> --create-db`)
}

func printTestUsage() {
	fmt.Println(`Usage: gofastr test [--run=<regex>] [--bench=<regex>] [--race] [--cover] [--short] [--timeout=<d>]

Runs the project's tests (go test ./...), colorizing the output.
GOFASTR_TEST=1 is set for the child process.

--run=<regex>     run only tests matching the regex
--bench=<regex>   also run matching benchmarks
--race            enable the race detector
--cover           report coverage
--short           set -short
--timeout=<d>     per-package timeout (e.g. --timeout=10m)`)
}

func printHarnessUsage() {
	fmt.Println(`Usage: gofastr harness [flags]
       gofastr harness mcp               launch the harness as a stdio MCP server
       gofastr harness creds [add|list|delete]  manage encrypted API-key credentials

Boots the AI agent harness. Flags select the profile, transports, and
permission posture; see the harness architecture docs for the full list.`)
}

func printAgentsUsage() {
	fmt.Println(`Usage: gofastr agents [init|sync|skill]

Generate or refresh AGENTS.md and the per-battery detail files.

  init    write a fresh AGENTS.md (errors if it exists; use 'sync' to refresh)
  sync    regenerate the battery inventory section in an existing AGENTS.md
  skill   emit a host-skill snippet for an agent that edits this project`)
}
