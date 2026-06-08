package main

import (
	"fmt"
	"os"
	"strings"
)

// Version info — overridden via -ldflags at build time.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// osExit is an indirection over os.Exit so tests can observe the exit
// code of CLI entry points without terminating the test binary. In
// production it is os.Exit verbatim — behavior is identical. Tests swap
// it (and restore via the helper) to capture the requested exit status.
var osExit = os.Exit

// stdoutIsTTY is true when os.Stdout is connected to a terminal. ANSI
// color helpers below return their input unchanged when this is false,
// so `gofastr docs | less` or `gofastr docs > out.txt` don't get
// littered with escape sequences. Evaluated once at startup —
// re-opening stdout post-init doesn't re-detect.
var stdoutIsTTY = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}()

// ANSI color helpers
func green(msg string) string {
	if !stdoutIsTTY {
		return msg
	}
	return "\033[32m" + msg + "\033[0m"
}

func red(msg string) string {
	if !stdoutIsTTY {
		return msg
	}
	return "\033[31m" + msg + "\033[0m"
}

func yellow(msg string) string {
	if !stdoutIsTTY {
		return msg
	}
	return "\033[33m" + msg + "\033[0m"
}

func bold(msg string) string {
	if !stdoutIsTTY {
		return msg
	}
	return "\033[1m" + msg + "\033[0m"
}

func dim(msg string) string {
	if !stdoutIsTTY {
		return msg
	}
	return "\033[2m" + msg + "\033[0m"
}

func success(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", green("✓"), fmt.Sprintf(format, args...))
}

func fail(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", red("✗"), fmt.Sprintf(format, args...))
}

func info(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", yellow("→"), fmt.Sprintf(format, args...))
}

func warn(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", yellow("⚠"), fmt.Sprintf(format, args...))
}

func printHelp() {
	fmt.Printf(`
%s — GoFastr CLI %s

%s:
  gofastr <command> [arguments]

%s:
  init <name>           Scaffold a new GoFastr project
    Flags:
      --module=<path>  Set Go module path (default: local/<name>)
      --no-entity      Skip sample entity scaffolding
      --db=<driver>    Database driver: sqlite (default) or postgres
  generate entity <n>   Generate an entity definition file
  new entity <n>        Scaffold a new entity (lower-level than kiln)
  new handler <n>       Scaffold a new HTTP handler
  new route <path>      Scaffold a route registration
  generate --from=<yml> Generate code from a deterministic YAML blueprint
  generate --config=<yml> Run YAML-configured code generators/extensions
  theme init            Scaffold theme/theme.go for a UI project
  build                 Run codegen + go build
  dev                   Start dev server with auto-restart
  migrate [up|down|status]  Run database migrations
  test                  Run project tests
  embed <sub>           Local semantic index (index/watch/query/stats/clear)
  harness               Start the AI agent harness (interactive loop / TUI)
    mcp                 Launch harness as a stdio MCP server for IDE integration
    creds [add|list|delete]  Manage encrypted API-key credentials
  agents [init|sync|skill]  Generate/refresh AGENTS.md and per-battery detail files
  docs [topic]          Browse framework docs (auto-versioned with this binary)
                        --list  list every topic; --grep <term> search across docs
  audit <sub>           Inspect the project for security-relevant patterns
                        deps  list packages that perform init-time global registrations
  version               Print version info

%s:
  --help, -h      Show this help message
  --version, -v   Print version and exit

%s:
  gofastr init myapp
  gofastr init myapp --module=github.com/me/myapp
  gofastr init myapp --no-entity
  gofastr generate entity user name:string email:string:unique
  gofastr generate --from=gofastr.yml --dry-run
  gofastr generate --config=gofastr.codegen.yml
  gofastr theme init
  gofastr build
  gofastr dev
  gofastr migrate up
  gofastr test
  gofastr harness
  gofastr harness creds add openrouter default sk-or-v1-...
  gofastr harness creds list
  gofastr agents init
  gofastr agents sync
`, bold("GoFastr"), version, bold("Usage"), bold("Commands"), bold("Flags"), bold("Examples"))
}

func main() {
	dispatch(os.Args[1:])
}

// dispatch routes a parsed argument vector (os.Args[1:]) to the matching
// subcommand. main() is a thin wrapper so this dispatch logic is testable
// in-process; behavior is identical to inlining it in main().
func dispatch(args []string) {
	// No args → help
	if len(args) == 0 {
		printHelp()
		return
	}

	// Check for global flags anywhere in args
	for _, a := range args {
		switch a {
		case "--help", "-h":
			printHelp()
			return
		case "--version", "-v":
			fmt.Printf("GoFastr %s (commit: %s, built: %s)\n", version, commit, buildDate)
			return
		}
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "init":
		runInit(cmdArgs)
	case "theme":
		runTheme(cmdArgs)
	case "generate", "gen", "g":
		runGenerate(cmdArgs)
	case "build":
		runBuild(cmdArgs)
	case "dev":
		runDev(cmdArgs)
	case "new":
		runNew(cmdArgs)
	case "migrate", "m":
		runMigrate(cmdArgs)
	case "test", "t":
		runTest(cmdArgs)
	case "embed":
		runEmbed(cmdArgs)
	case "harness":
		runHarness(cmdArgs)
	case "docs", "doc":
		runDocs(cmdArgs)
	case "agents":
		runAgents(cmdArgs)
	case "audit":
		runAudit(cmdArgs)
	case "version":
		fmt.Printf("GoFastr %s (commit: %s, built: %s)\n", version, commit, buildDate)
	default:
		fmt.Printf("%s Unknown command: %s\n\n", red("✗"), cmd)
		// Fuzzy suggestion: check if it's close to a known command
		suggestions := []string{"init", "generate", "build", "dev", "migrate", "test", "embed", "harness", "docs", "agents", "audit", "version"}
		for _, s := range suggestions {
			if strings.HasPrefix(s, cmd) || levenshtein(cmd, s) <= 2 {
				fmt.Printf("  Did you mean: %s?\n", bold("gofastr "+s))
			}
		}
		printHelp()
		osExit(1)
	}
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
		}
	}
	return d[la][lb]
}

func min(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
