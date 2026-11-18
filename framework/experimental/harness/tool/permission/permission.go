package permission

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Decision is what the engine does with a tool call.
type Decision uint8

const (
	DecisionAsk Decision = iota
	DecisionAllow
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionDeny:
		return "deny"
	default:
		return "ask"
	}
}

// Rule maps a tool call to a Decision.
type Rule struct {
	// Tool is the tool name, or "*" for any.
	Tool string
	// ArgvGlob is the call's argv summary (the Bash command, or "Read:<path>" and the like); empty matches any.
	ArgvGlob string
	// Glob makes ArgvGlob a filepath.Match pattern instead of a literal.
	Glob bool `json:",omitempty"`
	// Action is the Decision when the rule matches.
	Action Decision
	// Dir is the working directory a persistent rule was granted in and applies in; other rules ignore it.
	Dir string `json:",omitempty"`
}

// Match reports whether the rule applies to a call of tool with argv summary argv.
func (r Rule) Match(tool, argv string) bool {
	if r.Tool != "*" && r.Tool != tool {
		return false
	}
	if r.ArgvGlob == "" {
		return true
	}
	if !r.Glob {
		return r.ArgvGlob == argv
	}
	ok, _ := filepath.Match(r.ArgvGlob, argv)
	return ok
}

// Engine decides tool calls; it is safe for concurrent use.
type Engine struct {
	mu              sync.RWMutex
	profileRules    []Rule
	sessionRules    map[ids.SessionID][]Rule
	persistentRules []Rule

	// PersistencePath is the JSON file holding the "Allow always" rules; empty keeps them in memory.
	PersistencePath string
	// QuietMode allows read-only calls on the preset's allow-list whose paths stay under WorkingDir.
	QuietMode bool
	// StrictPermissions asks for every call no rule decides, QuietMode notwithstanding.
	StrictPermissions bool
	// WorkingDir is the sessions' working directory, stamped on persistent rules and bounding quiet mode.
	WorkingDir string
}

// New returns an Engine with the given profile rules and QuietMode on.
func New(profileRules []Rule) *Engine {
	return &Engine{
		profileRules: profileRules,
		sessionRules: make(map[ids.SessionID][]Rule),
		QuietMode:    true,
	}
}

// Evaluate returns the Decision for a call: session rules, then persistent rules for WorkingDir, then profile rules, then quiet mode, else ask.
func (e *Engine) Evaluate(session ids.SessionID, toolName, argv string, mutating bool) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.sessionRules[session] {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	for _, r := range e.persistentRules {
		if r.Dir != e.WorkingDir {
			continue
		}
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	for _, r := range e.profileRules {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	if e.StrictPermissions {
		return DecisionAsk
	}
	if e.QuietMode && quietModeAllows(e.WorkingDir, toolName, argv) {
		return DecisionAllow
	}
	return DecisionAsk
}

// AddSessionRule adds a rule that applies to session only.
func (e *Engine) AddSessionRule(session ids.SessionID, r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionRules[session] = append(e.sessionRules[session], r)
}

// ListSessionRules returns a copy of session's rules.
func (e *Engine) ListSessionRules(session ids.SessionID) []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	src := e.sessionRules[session]
	out := make([]Rule, len(src))
	copy(out, src)
	return out
}

// RevokeSessionRule removes session's rule at index; an index out of range does nothing.
func (e *Engine) RevokeSessionRule(session ids.SessionID, index int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	src := e.sessionRules[session]
	if index < 0 || index >= len(src) {
		return
	}
	e.sessionRules[session] = append(src[:index], src[index+1:]...)
}

// AnswerToRule turns an answer scoped wider than once into a literal session Rule, or false for any other scope.
func AnswerToRule(toolName, argvSummary string, ans control.AnswerPermission) (Rule, bool) {
	act := DecisionAsk
	switch ans.Decision {
	case control.DecisionAllow:
		act = DecisionAllow
	case control.DecisionDeny:
		act = DecisionDeny
	}
	switch ans.Scope {
	case control.ScopeArgvGlob:
		return Rule{Tool: toolName, ArgvGlob: argvSummary, Action: act}, true
	case control.ScopeTool:
		return Rule{Tool: toolName, ArgvGlob: "", Action: act}, true
	case control.ScopeSessionWide:
		return Rule{Tool: "*", ArgvGlob: "", Action: act}, true
	default:
		return Rule{}, false
	}
}

func quietModeAllows(workingDir, toolName, argv string) bool {
	switch toolName {
	case "Read", "Glob", "Ls", "Grep":
		return quietPathsInside(workingDir, toolName, argv)
	case "Bash":
		return bashQuietAllow(argv) && quietPathsInside(workingDir, toolName, argv)
	}
	return false
}

var quietBashAllowed = []string{
	"git status",
	"git log",
	"git diff",
	"git branch",
	"git show",
	"git config --get",
	"ls",
	"pwd",
	"cat ",
	"head ",
	"tail ",
	"wc ",
	"grep ",
	"rg ",
	"find ",
	"echo ",
}

const bashShellMetachars = ";&|`$<>()\n\r"

var execArgFlags = map[string]bool{
	"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
	"-delete": true, "-fprintf": true, "-fls": true, "-fprint": true,
	"--pre": true, "--hostname-bin": true, "--output": true,
	"-o": true,
}

var execArgPrefixes = []string{"-fprint", "-fls", "-exec", "-ok"}

var shellQuoting = strings.NewReplacer(`'`, "", `"`, "", `\`, "")

func hasExecArg(cmd string) bool {
	for tok := range strings.FieldsSeq(cmd) {
		tok = shellQuoting.Replace(tok)
		if name, _, ok := strings.Cut(tok, "="); ok {
			tok = name
		}
		if execArgFlags[tok] {
			return true
		}
		for _, p := range execArgPrefixes {
			if strings.HasPrefix(tok, p) {
				return true
			}
		}
	}
	return false
}

func bashQuietAllow(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	if strings.ContainsAny(cmd, bashShellMetachars) {
		return false
	}
	if hasExecArg(cmd) {
		return false
	}
	for _, p := range quietBashAllowed {
		trimmed := strings.TrimRight(p, " ")
		if cmd == trimmed {
			return true
		}
		if rest, ok := strings.CutPrefix(cmd, trimmed+" "); ok {
			if trimmed == "git branch" {
				return gitBranchLists(rest)
			}
			return true
		}
	}
	return false
}

var gitBranchListing = map[string]bool{
	"-a": true, "--all": true, "-r": true, "--remotes": true,
	"-v": true, "-vv": true, "--verbose": true, "--show-current": true,
	"-l": true, "--list": true, "--no-color": true,
}

func gitBranchLists(args string) bool {
	fields := strings.Fields(args)
	list := slices.Contains(fields, "--list") || slices.Contains(fields, "-l")
	for _, f := range fields {
		if strings.HasPrefix(f, "-") {
			if !gitBranchListing[f] {
				return false
			}
		} else if !list {
			return false
		}
	}
	return true
}
