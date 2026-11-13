// Command gofastr is the GoFastr CLI. main parses the subcommand and
// dispatches to the package under internal/ that implements it; each
// subcommand's code and tests live there, so a change to one selects
// only that package's tests and the packages that import it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/agentscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/auditcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/buildcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/desktopcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/docscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/exportcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/generatecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/harnesscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/initcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/migratecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/newcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/packcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/releasecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/semanticcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/testcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/themecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/unmountcmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/upgradecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/validatecmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/verifycmd"
	"github.com/DonaldMurillo/gofastr/core/fuzzy"
)

// Version info: overridden via -ldflags at build time
// (-X main.version=…). main hands them to cli, where every subcommand
// reads them.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func printHelp() {
	fmt.Printf(`
%s: GoFastr CLI %s

%s:
  gofastr <command> [arguments]

%s:
  init <name>           Scaffold a new GoFastr project
    Flags:
      --module=<path>  Set Go module path (default: local/<name>)
      --no-entity      Skip sample entity scaffolding
      --db=<driver>    Database driver: sqlite (default) or postgres (aliases: sqlite3, postgresql)
  new handler <n>       Scaffold a new HTTP handler
  new route <path>      Scaffold a route registration
  generate (gen, g)     Run the derived generators in the codegen config (gofastr generate --help lists all)
  generate app [spec]   Generate an app from a gofastr.yml app spec
  generate cli          Generate a customer-facing CLI for the app's API (docs: gofastr docs app-cli)
  generate styles [patterns] Generate typed Go for app tokens and owned style sheets
  pack [app-dir]        Snapshot a generated app into a best-effort gofastr.yml (lossy; not an inverse of generate)
  validate <yml>        Validate an app spec without generating (exit 0 = valid)
  theme init            Scaffold theme/theme.go for a UI project
  theme edit            Live theme configurator with a component preview
  build                 Run codegen + go vet + accessibility lint + go build
                        --no-a11y skips the accessibility gate
                        --no-embed-check skips the embed server-action gate
                        --allow-unverified-embeds keeps proven violations fatal
                        but downgrades an embed surface the analyzer cannot
                        follow
                        --pkg=<path> selects the main package built after the
                        project-root vet and accessibility gates (default .);
                        for example --pkg ./cmd/server
  dev                   Start dev server with auto-restart
    Flags:
      --addr=<host:port>  Listen address (default localhost:8080); -p <port> short form
      --dir=<path>     Project root: watch root and server cwd (default .)
      --pkg=<path>     Package to build, relative to --dir (default .). Use
                       --pkg ./cmd/<name> when main lives under cmd/, so the
                       watcher still sees internal/ and relative paths resolve
                       against the project root
  export <dir>          Render every page into <dir> as a static site
                        --base=<subpath> serves it under a URL subpath;
                        --pkg=<path> selects the main package (default .)
  migrate (m) [up|down|status|generate|force|repair]  Run database migrations
  test (t)              Run project tests
  semantic <sub>        Local semantic index (index/watch/query/stats/clear): experimental
  harness               Start the AI agent harness (interactive loop / TUI): experimental
    mcp                 Launch harness as a stdio MCP server for IDE integration
    creds [add|list|delete]  Manage encrypted API-key credentials
  agents [init|sync|skill]  Generate/refresh AGENTS.md and per-battery detail files
  desktop run|build|types  Build/run a GoFastr app as a desktop window (experimental):
                        run  build + launch with dev tools; build  .app on macOS or
                        .exe + WebView2Loader.dll on Windows/amd64; types  desktop.d.ts
  release --version=<v>  Cross-build a CLI or service binary for macOS, Linux and
                        Windows; write archives, checksums.txt and (with --url)
                        install.sh + install.ps1. See "gofastr docs release"
  docs (doc) [topic]    Browse framework docs (auto-versioned with this binary)
                        --list  list every topic; --grep <term> search across docs
  verify [capability]   Check the app against the GoFastr contract: routing, permissions,
                        security, data, entities, architecture, rendering, accessibility,
                        performance, testing, ai. Strict by default; relax in
                        gofastr.settings.yml or waive a line with //gofastr:allow(RULE).
                        --list catalog · --explain <rule> · --json · --sarif <file> · --fix
  audit <sub>           Inspect the project for security- and accessibility-relevant patterns
                        deps  list packages that perform init-time global registrations
                        lint  scan for AI-typical mistakes (ignored Exec, missing CSRF, …)
                        a11y  guided accessibility lint; --url <base> runs the full
                              axe-core scan against a running app (both color schemes)
  upgrade               Guide the app to a newer GoFastr release: shows every
                        migration note between go.mod's version and the target,
                        points at affected lines; --apply runs the go get/tidy/
                        build/test steps. See "gofastr docs upgrading"
  unmount [<package>]   Copy a gofastr package into the app and rewire its imports
                        to the copy; no package lists the copies. See "gofastr docs unmount"
  remount <package>     Go back to the framework's package and delete the copy
  version               Print version info
  help [command]        This overview, or the command's own --help

%s:
  --help, -h      Show this help message
  --version, -v   Print version and exit

%s:
  gofastr init myapp
  gofastr init myapp --module=github.com/me/myapp
  gofastr init myapp --no-entity
  gofastr validate gofastr.yml
  gofastr generate app gofastr.yml --dry-run
  gofastr generate --config=gofastr.settings.yml
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
  gofastr desktop run --pkg ./examples/desktop-notes
  gofastr verify
  gofastr verify security --json
  gofastr verify --explain GOFASTR1002
`, cli.Bold("GoFastr"), cli.ScrubTerminalOutput(project.CliVersion()), cli.Bold("Usage"), cli.Bold("Commands"), cli.Bold("Flags"), cli.Bold("Examples"))
}

func main() {
	verifycmd.RunAsVettoolIfMarked()
	cli.Version, cli.Commit, cli.BuildDate = version, commit, buildDate
	dispatch(os.Args[1:])
}

type commandSpec struct {
	names          []string
	run            func([]string)
	ownsHelp       bool
	versionChecked bool
	json           func([]string) bool
}

var commands = []commandSpec{
	{names: []string{"init"}, run: initcmd.RunInit, ownsHelp: true},
	{names: []string{"new"}, run: newcmd.RunNew, ownsHelp: true, versionChecked: true},
	{names: []string{"generate", "gen", "g"}, run: generatecmd.RunGenerate, ownsHelp: true, versionChecked: true, json: func(args []string) bool {
		return slices.Contains(args, "--json") && (cli.HasHelpFlagBeforeTerminator(args) || !slices.Contains(args, "--watch"))
	}},
	{names: []string{"pack"}, run: packcmd.RunPack, ownsHelp: true, versionChecked: true},
	{names: []string{"validate"}, run: validatecmd.RunValidate, ownsHelp: true, versionChecked: true},
	{names: []string{"build"}, run: buildcmd.RunBuild, ownsHelp: true, versionChecked: true},
	{names: []string{"dev"}, run: devcmd.RunDev, ownsHelp: true, versionChecked: true},
	{names: []string{"export"}, run: exportcmd.RunExport, ownsHelp: true, versionChecked: true},
	{names: []string{"migrate", "m"}, run: migratecmd.RunMigrate, ownsHelp: true, versionChecked: true},
	{names: []string{"test", "t"}, run: testcmd.RunTest, ownsHelp: true, versionChecked: true},
	{names: []string{"semantic"}, run: semanticcmd.RunSemantic, ownsHelp: true, versionChecked: true, json: func(args []string) bool {
		return len(args) > 0 && slices.Contains([]string{"query", "q", "stats"}, args[0])
	}},
	{names: []string{"harness"}, run: harnesscmd.RunHarness, ownsHelp: true, versionChecked: true},
	{names: []string{"docs", "doc"}, run: docscmd.RunDocs, ownsHelp: true},
	{names: []string{"agents"}, run: agentscmd.RunAgents, ownsHelp: true, versionChecked: true},
	{names: []string{"desktop"}, run: desktopcmd.RunDesktop, ownsHelp: true, versionChecked: true},
	{names: []string{"release"}, run: releasecmd.RunRelease, ownsHelp: true, versionChecked: true},
	{names: []string{"audit"}, run: auditcmd.RunAudit, ownsHelp: true, versionChecked: true},
	{names: []string{"verify"}, run: verifycmd.RunVerify, ownsHelp: true, versionChecked: true, json: func(args []string) bool {
		return slices.Contains(args, "--json")
	}},
	{names: []string{"upgrade"}, run: upgradecmd.RunUpgrade, ownsHelp: true},
	{names: []string{"unmount"}, run: unmountcmd.RunUnmount, ownsHelp: true, versionChecked: true},
	{names: []string{"remount"}, run: unmountcmd.RunRemount, ownsHelp: true, versionChecked: true},
	{names: []string{"theme"}, run: themecmd.RunTheme, ownsHelp: true, versionChecked: true},
	{names: []string{"version"}, run: func([]string) { cli.Println(project.VersionLine()) }},
	{names: []string{"help"}, run: func([]string) { printHelp() }},
}

func findCommand(name string) *commandSpec {
	for index := range commands {
		if slices.Contains(commands[index].names, name) {
			return &commands[index]
		}
	}
	return nil
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

	// Global flags. --version/-v prints the version in the command's
	// place only; after a command it is that command's argument (verify's
	// -v, a desktop build's --version, and `test -v`, which go test users
	// type and which must be refused rather than answered with the version
	// and exit 0). --help/-h is routed through to subcommands that own help,
	// which implement it; for every other command it is intercepted
	// anywhere in args (a side-effectful command like `dev --help` must
	// never start a server because it lacks its own help path).
	if isGlobalVersionFlag(args) {
		cli.Println(project.VersionLine())
		return
	}
	command := findCommand(args[0])
	if command == nil || !command.ownsHelp {
		for _, a := range args {
			if a == "--help" || a == "-h" {
				printHelp()
				return
			}
		}
	}

	cmd := args[0]
	cmdArgs := args[1:]
	// `gofastr help` is the overview and `gofastr help <command>` is
	// `gofastr <command> --help`. An unknown name falls through to the
	// unknown-command answer below.
	if cmd == "help" {
		if len(cmdArgs) == 0 {
			printHelp()
			return
		}
		if findCommand(cmdArgs[0]) != nil {
			dispatch([]string{cmdArgs[0], "--help"})
			return
		}
		cmd = cmdArgs[0]
		command = nil
	}
	if command != nil && command.json != nil && command.json(cmdArgs) {
		defer cli.BeginJSON()()
		if cli.HasHelpFlagBeforeTerminator(cmdArgs) {
			command.run(cmdArgs)
			return
		}
	}
	warnVersionMismatch(cmd, cmdArgs)

	if command != nil {
		command.run(cmdArgs)
	} else {
		cli.Fail("Unknown command: %s", cmd)
		fmt.Println()
		// Fuzzy suggestion: check if it's close to a known command
		for _, candidate := range commands {
			s := candidate.names[0]
			if strings.HasPrefix(s, cmd) || fuzzy.Levenshtein(cmd, s) <= 2 {
				cli.Printf("  Did you mean: "+cli.Bold("gofastr %s")+"?\n", s)
			}
		}
		printHelp()
		cli.OsExit(1)
	}
}

// versionCheckApplies reports whether warnVersionMismatch speaks for
// this command line: a project command, not a help request.
func versionCheckApplies(cmd string, args []string) bool {
	command := findCommand(cmd)
	if command == nil || !command.versionChecked {
		return false
	}
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return false
		}
	}
	return true
}

// warnVersionMismatch prints, on stderr, a warning when the project in
// the working directory requires a different GoFastr release than this
// CLI (project.VersionMismatch). It never stops the command.
func warnVersionMismatch(cmd string, args []string) {
	if !versionCheckApplies(cmd, args) {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	if dir := projectDirFlag(cmd, args); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(wd, dir)
		}
		wd = dir
	}
	if w := project.VersionMismatch(wd); w != "" {
		cli.Fprintf(os.Stderr, cli.Yellow("warning:")+" %s\n", w)
	}
}

// projectDirFlag is the project directory a command was pointed at
// with --dir (`gofastr dev --dir=../app`), or "" when it works in the
// current directory.
func projectDirFlag(cmd string, args []string) string {
	if cmd != "dev" {
		return ""
	}
	// The last --dir wins, as in dev's own flag parser.
	dir := ""
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--dir="); ok {
			dir = v
		} else if a == "--dir" && i+1 < len(args) {
			dir = args[i+1]
		}
	}
	return dir
}

// isGlobalVersionFlag reports whether argv asks for the version as a
// global flag: --version or -v where the command name goes.
func isGlobalVersionFlag(args []string) bool {
	return len(args) > 0 && (args[0] == "--version" || args[0] == "-v")
}
