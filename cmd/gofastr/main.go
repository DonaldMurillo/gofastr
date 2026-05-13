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

// ANSI color helpers
func green(msg string) string {
	return "\033[32m" + msg + "\033[0m"
}

func red(msg string) string {
	return "\033[31m" + msg + "\033[0m"
}

func yellow(msg string) string {
	return "\033[33m" + msg + "\033[0m"
}

func bold(msg string) string {
	return "\033[1m" + msg + "\033[0m"
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

func printHelp() {
	fmt.Printf(`
%s — GoFastr CLI %s

%s:
  gofastr <command> [arguments]

%s:
  init <name>           Scaffold a new GoFastr project
  generate entity <n>   Generate an entity definition file
  theme init            Scaffold theme/theme.go for a UI project
  build                 Run codegen + go build
  dev                   Start dev server with auto-restart
  migrate [up|down|status]  Run database migrations
  test                  Run project tests
  embed <sub>           Local semantic index (index/watch/query/stats/clear)
  version               Print version info

%s:
  --help, -h      Show this help message
  --version, -v   Print version and exit

%s:
  gofastr init myapp
  gofastr generate entity user name:string email:string:unique
  gofastr theme init
  gofastr build
  gofastr dev
  gofastr migrate up
  gofastr test
`, bold("GoFastr"), version, bold("Usage"), bold("Commands"), bold("Flags"), bold("Examples"))
}

func main() {
	args := os.Args[1:]

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
	case "migrate", "m":
		runMigrate(cmdArgs)
	case "test", "t":
		runTest(cmdArgs)
	case "embed":
		runEmbed(cmdArgs)
	case "version":
		fmt.Printf("GoFastr %s (commit: %s, built: %s)\n", version, commit, buildDate)
	default:
		fmt.Printf("%s Unknown command: %s\n\n", red("✗"), cmd)
		// Fuzzy suggestion: check if it's close to a known command
		suggestions := []string{"init", "generate", "build", "dev", "migrate", "test", "embed", "version"}
		for _, s := range suggestions {
			if strings.HasPrefix(s, cmd) || levenshtein(cmd, s) <= 2 {
				fmt.Printf("  Did you mean: %s?\n", bold("gofastr "+s))
			}
		}
		printHelp()
		os.Exit(1)
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
