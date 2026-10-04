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
// module name the built binaries a launch could refer to.
func collectSignals(calls []shimCall, events []toolEvent, workspace, module string) Signals {
	var s Signals
	shimDev, devBuilds := 0, 0
	for _, c := range calls {
		if isDevBuild(c) {
			devBuilds++
			continue
		}
		if !c.FromAgent() {
			continue
		}
		switch kind, detail := classifyArgv(c.Tool, c.Args); kind {
		case launchDev:
			shimDev++
		case launchGoRun:
			s.GoRuns = append(s.GoRuns, detail)
		}
	}

	built := map[string]bool{path.Base(module): true}
	transcriptDev, transcriptGoRuns := 0, []string(nil)
	for _, e := range events {
		if e.Name != "Bash" || e.Command == "" {
			continue
		}
		s.Commands = append(s.Commands, e.Command)
		for _, argv := range simpleCommands(e.Command) {
			tool := path.Base(argv[0])
			switch kind, detail := classifyArgv(tool, argv[1:]); kind {
			case launchDev:
				transcriptDev++
			case launchGoRun:
				transcriptGoRuns = append(transcriptGoRuns, detail)
			case buildOutput:
				built[detail] = true
			default:
				if isBinaryLaunch(argv[0], workspace, built) {
					s.BinaryLaunches = append(s.BinaryLaunches, strings.Join(argv, " "))
				}
			}
		}
	}
	s.DevLaunches = max(shimDev, transcriptDev)
	s.DevRebuilds = max(devBuilds-s.DevLaunches, 0)
	if len(transcriptGoRuns) > len(s.GoRuns) {
		s.GoRuns = transcriptGoRuns
	}
	return s
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
// PATH, so it counts as a dev launch, not a bypass.
func classifyArgv(tool string, args []string) (launchKind, string) {
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
			return launchGoRun, "go " + joined
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

func firstPositional(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// wantsHelp reports a help request: `gofastr dev --help` prints usage
// and exits without serving anything.
func wantsHelp(args []string) bool {
	return containsWord(args, "-h") || containsWord(args, "--help") || containsWord(args, "-help")
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

// simpleCommands splits a shell line into argv lists. It is a grader's
// approximation, not a shell parser: it honors quotes, backslashes and
// comments, splits on control operators and subshell brackets outside
// quotes, drops leading env assignments and wrapper commands, and skips
// `cd`. Quote awareness matters: `grep "x\|gofastr dev"` names the
// command in a pattern and must not count as a launch.
func simpleCommands(line string) [][]string {
	var (
		out     [][]string
		words   []string
		word    strings.Builder
		inWord  bool
		quote   rune
		escaped bool
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
		if fields := stripPrefixWords(words); len(fields) > 0 && fields[0] != "cd" {
			out = append(out, fields)
		}
		words = nil
	}
	runes := []rune(line)
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
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			endCommand()
		case r == '&' && i > 0 && (runes[i-1] == '>' || runes[i-1] == '<'):
			// 2>&1 and friends: a redirect, not a background operator.
			word.WriteRune(r)
			inWord = true
		case strings.ContainsRune(";|&\n(){}", r):
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

// isBinaryLaunch reports whether word runs a compiled program: a path
// whose base name the agent built, or a path to a file in the workspace
// that carries an executable header. Scripts stay out; they are judged
// by whatever they run, which the shim records.
func isBinaryLaunch(word, workspace string, built map[string]bool) bool {
	if !strings.Contains(word, "/") {
		return false
	}
	if built[filepath.Base(word)] {
		return true
	}
	p := word
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
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
