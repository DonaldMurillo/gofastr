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

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
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
	// What to do when matched.
	Action Decision
}

// Match reports whether the rule applies to a given (tool, argv) pair.
func (r Rule) Match(tool, argv string) bool {
	if r.Tool != "*" && r.Tool != tool {
		return false
	}
	if r.ArgvGlob == "" {
		return true
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

	// 1) Session-scoped rules (most specific).
	for _, r := range e.sessionRules[session] {
		if r.Match(toolName, argv) {
			return r.Action
		}
	}
	// 2) Profile-level rules.
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

func bashQuietAllow(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, p := range quietBashAllowed {
		if cmd == strings.TrimRight(p, " ") || strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}
