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

// infoString is info's message formatted for a caller-owned writer —
// for goroutines that must not read the os.Stdout global (see dev.go's
// crash watcher).
func infoString(format string, args ...interface{}) string {
	return fmt.Sprintf("  %s %s", yellow("→"), fmt.Sprintf(format, args...))
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
  new handler <n>       Scaffold a new HTTP handler
  new route <path>      Scaffold a route registration
  generate --from=<yml> Generate code from a deterministic YAML blueprint
  generate --config=<yml> Run YAML-configured code generators/extensions
  pack [app-dir]        Snapshot a generated app into a best-effort gofastr.yml (lossy; not an inverse of generate)
  validate <yml>        Validate a blueprint without generating (exit 0 = valid)
  theme init            Scaffold theme/theme.go for a UI project
  build                 Run codegen + go vet + accessibility lint + go build
                        --no-a11y skips the accessibility gate
  dev                   Start dev server with auto-restart
    Flags:
      --addr=<host:port>  Listen address (default localhost:8080); -p <port> short form
      --dir=<path>     Project root: watch root and server cwd (default .)
      --pkg=<path>     Package to build, relative to --dir (default .). Use
                       --pkg ./cmd/<name> when main lives under cmd/, so the
                       watcher still sees internal/ and relative paths resolve
                       against the project root
  migrate [up|down|status|generate|force]  Run database migrations
  test                  Run project tests
  embed <sub>           Local semantic index (index/watch/query/stats/clear)
  harness               Start the AI agent harness (interactive loop / TUI)
    mcp                 Launch harness as a stdio MCP server for IDE integration
    creds [add|list|delete]  Manage encrypted API-key credentials
  agents [init|sync|skill]  Generate/refresh AGENTS.md and per-battery detail files
  docs [topic]          Browse framework docs (auto-versioned with this binary)
                        --list  list every topic; --grep <term> search across docs
  audit <sub>           Inspect the project for security- and accessibility-relevant patterns
                        deps  list packages that perform init-time global registrations
                        lint  scan for AI-typical mistakes (ignored Exec, missing CSRF, …)
                        a11y  guided accessibility lint; --url <base> runs the full
                              axe-core scan against a running app (both color schemes)
  upgrade               Guide the app to a newer GoFastr release: shows every
                        migration note between go.mod's version and the target,
                        points at affected lines; --apply runs the go get/tidy/
                        build/test steps. See "gofastr docs upgrading"
  version               Print version info

%s:
  --help, -h      Show this help message
  --version, -v   Print version and exit

%s:
  gofastr init myapp
  gofastr init myapp --module=github.com/me/myapp
  gofastr init myapp --no-entity
  gofastr validate gofastr.yml
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

// ownsHelp lists the subcommands that implement their own --help/-h
// output; dispatch routes help flags through to them instead of
// printing the global overview. A command may only join this set once
// it actually handles the flags (otherwise `<cmd> --help` would run
// the command).
var ownsHelp = map[string]bool{
	"audit":   true,
	"upgrade": true,
	"docs":    true,
	"doc":     true,
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

	// Global flags. --version/-v is intercepted anywhere for EVERY
	// command (no subcommand defines its own). --help/-h is routed
	// through to subcommands in ownsHelp, which implement it; for every
	// other command it is intercepted anywhere in args (a side-effectful
	// command like `dev --help` must never start a server because it
	// lacks its own help path).
	for _, a := range args {
		if a == "--version" || a == "-v" {
			fmt.Printf("GoFastr %s (commit: %s, built: %s)\n", version, commit, buildDate)
			return
		}
	}
	if !ownsHelp[args[0]] {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				printHelp()
				return
			}
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
	case "pack":
		runPack(cmdArgs)
	case "validate":
		runValidate(cmdArgs)
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
	case "upgrade":
		runUpgrade(cmdArgs)
	case "version":
		fmt.Printf("GoFastr %s (commit: %s, built: %s)\n", version, commit, buildDate)
	default:
		fmt.Printf("%s Unknown command: %s\n\n", red("✗"), cmd)
		// Fuzzy suggestion: check if it's close to a known command
		suggestions := []string{"init", "generate", "pack", "validate", "build", "dev", "migrate", "test", "embed", "harness", "docs", "agents", "audit", "upgrade", "version"}
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
