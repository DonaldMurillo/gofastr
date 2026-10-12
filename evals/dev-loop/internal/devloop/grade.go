package devloop

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxDevLaunches allows one relaunch of `gofastr dev` (a port clash, a
// crashed first attempt). More than that means the agent is restarting
// the server after edits instead of letting the watcher rebuild.
const maxDevLaunches = 2

// minDevRebuilds is how many times `gofastr dev` must rebuild the app
// after its first build. The task has three sequential changes, so an
// agent working under the watcher triggers at least two rebuilds; one
// that starts the dev server only after the last edit, to look once,
// triggers none. Rebuilds are counted from the shim log rather than from
// the transcript's edit calls because agents edit through the shell
// (`sed -i`, heredocs) as often as through an edit tool.
const minDevRebuilds = 2

// devServerBinary is the temp-dir prefix `gofastr dev` builds the app
// into. It identifies a dev-loop build even when the agent launched
// `gofastr dev` through a path that skipped the shim.
const devServerBinary = "gofastr-dev-server-"

// Signals is the behavioral evidence for one trial.
type Signals struct {
	// DevLaunches counts agent-started `gofastr dev` processes: the larger
	// of the shim log (catches scripts) and the transcript (catches a
	// call through an absolute path that skips the shim).
	DevLaunches int `json:"dev_launches"`
	// GoRuns and BinaryLaunches are the bypasses: starting the app in a
	// way that never sets GOFASTR_DEV=1, so nothing reloads.
	GoRuns         []string `json:"go_runs,omitempty"`
	BinaryLaunches []string `json:"binary_launches,omitempty"`
	// DevRebuilds counts the builds `gofastr dev` ran beyond the first
	// one per launch: each is the watcher picking up an edit.
	DevRebuilds int `json:"dev_rebuilds"`
	// CallsBeforeDev is how many tool calls the agent made before its
	// first `gofastr dev` launch (-1: it never launched one), and Lookups
	// are the ones that went looking for how to run the app: an agents/
	// doc, `gofastr --help`, `gofastr docs`. Guidance that puts the dev
	// loop where the agent reads first shows up here, not in the
	// verdict: a model that finds the command after a search still passes.
	CallsBeforeDev int      `json:"calls_before_dev"`
	Lookups        []string `json:"lookups,omitempty"`
	// Commands is every shell command, for auditing a verdict by hand.
	Commands []string `json:"commands,omitempty"`
}

// Verdict is the call for one trial. Pass is the dev-loop verdict, the
// thing this eval measures, and the only one that sets the exit code.
// TaskDone is whether the finished app has the three changes: a model
// that guesses a field the component does not have fails the task, not
// the dev loop, and the report keeps the two apart.
type Verdict struct {
	Pass         bool     `json:"pass"`
	Failures     []string `json:"failures,omitempty"`
	TaskDone     bool     `json:"task_done"`
	TaskFailures []string `json:"task_failures,omitempty"`
}

// collectSignals merges the shim log and the transcript. workspace and
// module name the app a `go run` or a built binary could be.
//
// The two sources overlap only partly. A bare `gofastr dev` or `go run`
// typed in the agent's shell lands in both; one run from a script lands
// only in the shim log; one called by path (/opt/bin/gofastr,
// /usr/local/go/bin/go) skips the shim and lands only in the transcript.
// So a count is max(shim, transcript bare-name) plus transcript by-path.
func collectSignals(calls []shimCall, events []toolEvent, workspace, module string) Signals {
	s := Signals{CallsBeforeDev: -1}
	shimDev := 0
	var shimGoRuns []string
	// Each launch's first build is its startup; every later build until
	// the next launch is the watcher rebuilding after an edit. Builds
	// before any logged launch belong to a launch that skipped the shim.
	initialPending := true
	for _, c := range calls {
		if isDevBuild(c) {
			if initialPending {
				initialPending = false
			} else {
				s.DevRebuilds++
			}
			continue
		}
		if !c.FromAgent() {
			continue
		}
		switch kind, detail := classifyArgv(c.Tool, c.Args, workspace, module); kind {
		case launchDev:
			shimDev++
			initialPending = true
		case launchGoRun:
			shimGoRuns = append(shimGoRuns, detail)
		}
	}

	built := map[string]bool{path.Base(module): true}
	bareDev, pathDev := 0, 0
	var bareGoRuns, pathGoRuns []string
	for i, e := range events {
		launched := false
		if e.Name == "Bash" && e.Command != "" {
			s.Commands = append(s.Commands, e.Command)
			for _, argv := range simpleCommands(e.Command) {
				byPath := strings.Contains(argv[0], "/")
				switch kind, detail := classifyArgv(path.Base(argv[0]), argv[1:], workspace, module); kind {
				case launchDev:
					launched = true
					if byPath {
						pathDev++
					} else {
						bareDev++
					}
				case launchGoRun:
					if byPath {
						pathGoRuns = append(pathGoRuns, detail)
					} else {
						bareGoRuns = append(bareGoRuns, detail)
					}
				case buildOutput:
					built[detail] = true
				default:
					if isBinaryLaunch(argv[0], workspace, built) {
						s.BinaryLaunches = append(s.BinaryLaunches, strings.Join(argv, " "))
					}
				}
			}
		}
		if s.CallsBeforeDev < 0 {
			if launched {
				s.CallsBeforeDev = i
			} else if l := lookup(e); l != "" {
				s.Lookups = append(s.Lookups, l)
			}
		}
	}
	s.DevLaunches = max(shimDev, bareDev) + pathDev
	s.GoRuns = shimGoRuns
	if len(bareGoRuns) > len(s.GoRuns) {
		s.GoRuns = bareGoRuns
	}
	s.GoRuns = append(s.GoRuns, pathGoRuns...)
	return s
}

// lookup reports a tool call that went looking for how to run the app,
// as a short label, or "". AGENTS.md and CLAUDE.md are not lookups: the
// prompt tells the agent to read them.
func lookup(e toolEvent) string {
	label := func(s string) string {
		s = e.Name + " " + strings.ReplaceAll(s, "\n", " ")
		if len(s) > 120 {
			s = s[:120]
		}
		return s
	}
	if e.Name == "Bash" {
		// Label the matching simple command, not the whole line: agents
		// open with `cd <workspace>;`, which would fill the label.
		for _, argv := range simpleCommands(e.Command) {
			if cmd := strings.Join(argv, " "); strings.Contains(cmd, "agents/") {
				return label(cmd)
			}
			if path.Base(argv[0]) != "gofastr" {
				continue
			}
			// `gofastr dev --help` is not a lookup: an agent asking for
			// dev's flags has already chosen the command.
			sub := firstPositional(argv[1:])
			if sub == "" || sub == "help" || sub == "docs" || (sub != "dev" && wantsHelp(argv[1:])) {
				return label(strings.Join(argv, " "))
			}
		}
		if strings.Contains(e.Command, "agents/") {
			return label(e.Command)
		}
		return ""
	}
	if strings.Contains(filepath.ToSlash(e.Path), "agents/") {
		return label(e.Path)
	}
	return ""
}

// isDevBuild reports whether a logged `go build` is `gofastr dev`
// rebuilding the app.
func isDevBuild(c shimCall) bool {
	if c.Tool != "go" || len(c.Args) == 0 || c.Args[0] != "build" {
		return false
	}
	return c.Caller == "gofastr-dev" || strings.Contains(strings.Join(c.Args, " "), devServerBinary)
}

type launchKind int

const (
	launchNone launchKind = iota
	launchDev
	launchGoRun
	buildOutput
)

// classifyArgv recognizes the three command shapes grading cares about.
// `go run …/cmd/gofastr dev` is the dev loop reached without the CLI on
// PATH, so it counts as a dev launch, not a bypass. Any other `go run`
// is a bypass only when it runs the app itself: `go run ./scripts/seed`
// or `go run golang.org/x/tools/cmd/goimports@latest` runs a tool.
func classifyArgv(tool string, args []string, workspace, module string) (launchKind, string) {
	switch tool {
	case "gofastr":
		if firstPositional(args) == "dev" && !wantsHelp(args) {
			return launchDev, ""
		}
	case "go":
		if len(args) == 0 {
			return launchNone, ""
		}
		switch args[0] {
		case "run":
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "cmd/gofastr") && containsWord(args, "dev") {
				return launchDev, ""
			}
			if isAppTarget(goRunTarget(args[1:]), workspace, module) {
				return launchGoRun, "go " + joined
			}
		case "build":
			for i, a := range args {
				if a == "-o" && i+1 < len(args) {
					return buildOutput, filepath.Base(args[i+1])
				}
				if out, ok := strings.CutPrefix(a, "-o="); ok {
					return buildOutput, filepath.Base(out)
				}
			}
		}
	}
	return launchNone, ""
}

// goBuildValueFlags are the `go run` build flags that take their value
// as the next argument, so that argument is not the package.
var goBuildValueFlags = map[string]bool{
	"-C": true, "-exec": true, "-tags": true, "-ldflags": true, "-gcflags": true,
	"-asmflags": true, "-mod": true, "-modfile": true, "-overlay": true,
	"-pkgdir": true, "-toolexec": true, "-p": true, "-o": true, "-pgo": true,
	"-covermode": true, "-coverpkg": true,
}

// goRunTarget returns the package or first file a `go run` names.
func goRunTarget(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if goBuildValueFlags[a] {
			i++
		}
	}
	return ""
}

// isAppTarget reports whether a `go run` target is the scaffolded app's
// main package: the workspace directory, the module path, or Go files
// at the workspace root.
func isAppTarget(target, workspace, module string) bool {
	if target == "" {
		return false
	}
	if target == module {
		return true
	}
	dir := target
	if strings.HasSuffix(target, ".go") {
		dir = filepath.Dir(target)
	}
	dir = filepath.Clean(dir)
	return dir == "." || dir == filepath.Clean(workspace)
}

func firstPositional(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// wantsHelp reports a help request: `gofastr dev --help` prints usage
// and exits without serving anything. It mirrors the CLI's hasHelpFlag,
// which knows only --help and -h: `gofastr dev -help` ignores the
// unknown flag and serves.
func wantsHelp(args []string) bool {
	return containsWord(args, "-h") || containsWord(args, "--help")
}

func containsWord(args []string, word string) bool {
	for _, a := range args {
		if a == word {
			return true
		}
	}
	return false
}

// wrappers precede the real command without changing what runs.
var wrappers = map[string]bool{
	"nohup": true, "exec": true, "env": true, "setsid": true,
	"time": true, "command": true, "caffeinate": true,
}

// shells run their `-c` payload as shell code.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true}

// heredoc is a pending `<<DELIM` (or `<<-DELIM`) whose body starts at
// the next newline.
type heredoc struct {
	delim     string
	stripTabs bool
}

// simpleCommands splits a shell line into argv lists. It is a grader's
// approximation, not a shell parser: it honors quotes, backslashes and
// comments, splits on control operators and subshell brackets outside
// quotes, drops leading env assignments and wrapper commands, and skips
// `cd`. Quote awareness matters: `grep "x\|gofastr dev"` names the
// command in a pattern and must not count as a launch. Heredoc bodies
// are skipped as data (an apostrophe in one must not open a quote that
// swallows the commands after it), and a `sh -c` / `bash -c` payload is
// split as the shell code it is.
func simpleCommands(line string) [][]string {
	var (
		out      [][]string
		words    []string
		word     strings.Builder
		inWord   bool
		quote    rune
		escaped  bool
		heredocs []heredoc
	)
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		fields := stripPrefixWords(words)
		words = nil
		if len(fields) == 0 || fields[0] == "cd" {
			return
		}
		if shells[path.Base(fields[0])] {
			for j := 1; j+1 < len(fields); j++ {
				if a := fields[j]; strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c") {
					out = append(out, simpleCommands(fields[j+1])...)
					return
				}
			}
		}
		out = append(out, fields)
	}
	runes := []rune(line)
	// skipBodies moves i (on a newline) past every pending heredoc body,
	// leaving it on the last terminator line's newline.
	skipBodies := func(i int) int {
		for _, h := range heredocs {
			for i < len(runes) {
				end := i + 1
				for end < len(runes) && runes[end] != '\n' {
					end++
				}
				l := string(runes[i+1 : end])
				if h.stripTabs {
					l = strings.TrimLeft(l, "\t")
				}
				i = end
				if l == h.delim {
					break
				}
			}
		}
		heredocs = nil
		return i
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case escaped:
			escaped = false
			if r != '\n' {
				word.WriteRune(r)
				inWord = true
			}
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && i+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[i+1]):
				i++
				word.WriteRune(runes[i])
			default:
				word.WriteRune(r)
			}
		case r == '\\':
			escaped = true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '#' && !inWord:
			// Stop before the newline so the newline case below ends the
			// command and skips any heredoc body that follows.
			for i+1 < len(runes) && runes[i+1] != '\n' {
				i++
			}
		case r == '<' && i+2 < len(runes) && runes[i+1] == '<' && runes[i+2] == '<':
			// A here-string: the next word is data on this command.
			endWord()
			i += 2
		case r == '<' && i+1 < len(runes) && runes[i+1] == '<':
			endWord()
			i += 2
			h := heredoc{}
			if i < len(runes) && runes[i] == '-' {
				h.stripTabs = true
				i++
			}
			for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
				i++
			}
			var d strings.Builder
			for ; i < len(runes) && !strings.ContainsRune(" \t\n;|&()<>", runes[i]); i++ {
				if !strings.ContainsRune(`'"\`, runes[i]) {
					d.WriteRune(runes[i])
				}
			}
			h.delim = d.String()
			heredocs = append(heredocs, h)
			i-- // the loop's i++ lands on the delimiter's terminator
		case r == '\n':
			endCommand()
			if len(heredocs) > 0 {
				i = skipBodies(i)
			}
		case r == '&' && i > 0 && (runes[i-1] == '>' || runes[i-1] == '<'):
			// 2>&1 and friends: a redirect, not a background operator.
			word.WriteRune(r)
			inWord = true
		case strings.ContainsRune(";|&(){}", r):
			endCommand()
		case r == ' ' || r == '\t':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endCommand()
	return out
}

// stripPrefixWords drops env assignments and wrapper commands that sit
// in front of the command word.
func stripPrefixWords(fields []string) []string {
	for len(fields) > 0 {
		switch f := fields[0]; {
		case wrappers[f], isAssignment(f):
			fields = fields[1:]
		case f == "timeout" && len(fields) > 1:
			fields = fields[2:]
		default:
			return fields
		}
	}
	return fields
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// isBinaryLaunch reports whether word runs the app as a compiled
// program: a path whose base name the agent built, a path to a file in
// the workspace that carries an executable header, or a bare name the
// agent built that sits in the workspace (run through PATH=.:$PATH).
// System tools by absolute path (/usr/bin/curl) are not the app. A
// script is not a binary: the shims record the `go` and `gofastr` calls
// inside it, but a compiled binary a script starts reaches neither log.
func isBinaryLaunch(word, workspace string, built map[string]bool) bool {
	if !strings.Contains(word, "/") {
		return built[word] && hasExecutableHeader(filepath.Join(workspace, word))
	}
	if built[filepath.Base(word)] {
		return true
	}
	p := word
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
	}
	if rel, err := filepath.Rel(workspace, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return hasExecutableHeader(p)
}

var executableMagic = [][]byte{
	{0x7f, 'E', 'L', 'F'},
	{0xfe, 0xed, 0xfa, 0xce}, {0xfe, 0xed, 0xfa, 0xcf},
	{0xce, 0xfa, 0xed, 0xfe}, {0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe},
}

func hasExecutableHeader(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	for _, magic := range executableMagic {
		if bytes.Equal(head, magic) {
			return true
		}
	}
	return false
}

// judge turns signals plus the task probe into a verdict.
func judge(s Signals, taskFailures []string) Verdict {
	var failures []string
	if s.DevLaunches == 0 {
		failures = append(failures, "never ran `gofastr dev`")
	}
	if len(s.GoRuns) > 0 {
		failures = append(failures, fmt.Sprintf("ran the app with `go run` (%d time(s)), which never hot-reloads", len(s.GoRuns)))
	}
	if len(s.BinaryLaunches) > 0 {
		failures = append(failures, fmt.Sprintf("launched a built binary by hand (%d time(s)), which never hot-reloads", len(s.BinaryLaunches)))
	}
	if s.DevLaunches > maxDevLaunches {
		failures = append(failures, fmt.Sprintf("started `gofastr dev` %d times instead of letting it rebuild on save", s.DevLaunches))
	}
	if s.DevLaunches > 0 && s.DevRebuilds < minDevRebuilds {
		failures = append(failures, fmt.Sprintf("`gofastr dev` rebuilt %d time(s) after starting; the three edits should trigger at least %d", s.DevRebuilds, minDevRebuilds))
	}
	return Verdict{
		Pass: len(failures) == 0, Failures: failures,
		TaskDone: len(taskFailures) == 0, TaskFailures: taskFailures,
	}
}
