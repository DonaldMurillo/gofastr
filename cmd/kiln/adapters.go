package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// kilnSkillPath returns the absolute path to the kiln skill installed
// by installSkill(). Used by adapters that don't auto-discover skills
// from ~/.claude/skills/ (pi, codex) — they need an explicit flag
// pointing at the skill file.
func kilnSkillPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".claude", "skills", "kiln", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// kilnContractPreamble is the system-level "you are inside kiln" framing
// every adapter ships with the user's prompt. The world IR is the app —
// editing files on disk is meaningless and explicitly forbidden. Without
// this, claude/codex spawned in the user's cwd see a real Go repo and
// fall back to Read/Edit/Write on those files instead of dispatching
// kiln tool calls. The kiln SKILL.md describes *how* to call tools; this
// preamble establishes *that* tool calls are the only legal action.
const kilnContractPreamble = `You are running inside the Kiln runtime.

Kiln's world IR is the only source of truth for the app being built.
There is no on-disk codebase to read, edit, or compile. Any files you
see on the local filesystem are unrelated to this session and reading
or modifying them does NOTHING — the user is watching the live world,
not the disk.

The ONLY way to inspect or change the app is HTTP against $KILN_URL:

  - Read state:   curl -s "$KILN_URL/kiln/world"
  - Call a tool:  curl -s -X POST "$KILN_URL/kiln/tool/<name>" \
                       -H 'Content-Type: application/json' -d '<json args>'

Do NOT use Read, Write, Edit, or Glob/Grep. Do NOT cd, git, install
packages, or write source files. The Bash tool exists solely so you
can curl the kiln HTTP API. If you find yourself reaching for a file
tool, you are off-script — go back to curl.

The available tools are documented in the kiln skill. Read it before
acting.`

// kilnSystemPrompt returns the full per-turn system context for an
// agent: the contract preamble plus the SKILL.md tool surface (when
// installed). Adapters inject this via whatever system-prompt mechanism
// the underlying CLI provides (claude --append-system-prompt, codex
// prompt-prepend, pi --skill + prompt-prepend). Returning a single
// string keeps the contract identical across adapters.
func kilnSystemPrompt() string {
	out := kilnContractPreamble
	if p := kilnSkillPath(); p != "" {
		if buf, err := os.ReadFile(p); err == nil {
			out += "\n\n<kiln-skill>\n" + string(buf) + "\n</kiln-skill>"
		}
	}
	return out
}

// Adapter describes how to spawn a third-party CLI coding agent for one
// turn of conversation. Every adapter shares the same contract:
//
//   - Detect() returns true if the agent's binary is on PATH.
//   - BuildArgs(text) returns the argv to exec for one turn. The user's
//     prompt is the last argument; KILN_URL is injected via env upstream.
//
// Auth is the user's responsibility: each CLI manages its own login
// (e.g. `claude` reads ~/.claude/.credentials.json). Kiln does not
// touch credentials — it just spawns the binary.
type Adapter struct {
	Name      string
	Display   string // human-readable description for the startup banner
	Detect    func() bool
	BuildArgs func(text string) []string
	// Dir overrides the working directory for the spawned process.
	// Empty means inherit kiln's cwd. Set this for adapters whose
	// model is prone to reading the cwd (pi will cat any Go file
	// it sees and report on it as if it were the kiln world);
	// pointing them at a clean temp dir blocks that confusion.
	Dir string
}

// adapters is the built-in registry. Adding a new agent is a one-entry
// change here.
var adapters = map[string]Adapter{
	"claude-code": {
		Name:    "claude-code",
		Display: "claude --print --allowedTools Bash --append-system-prompt <kiln contract>  (Claude Code, reads ~/.claude/.credentials.json)",
		Dir:     filepath.Join(os.TempDir(), "kiln-claude"),
		Detect:  func() bool { _, err := exec.LookPath("claude"); return err == nil },
		BuildArgs: func(text string) []string {
			// --dangerously-skip-permissions is required for non-interactive
			// runs — without it claude --print hangs at the first Bash
			// tool-use waiting for a permission prompt that nobody can answer.
			//
			// --allowedTools Bash strips Read/Write/Edit/Glob/Grep so claude
			// has no on-disk fallback even if the model reaches for one.
			// curl is reached via Bash, which is all kiln needs.
			//
			// --append-system-prompt installs the kiln contract + skill at
			// the system level, so it outranks claude's default tool autonomy.
			// Without this, claude in --print mode auto-discovers the skill
			// at best advisorily — the model is still free to ignore it. As
			// a system prompt the contract is non-negotiable.
			return []string{
				"claude", "--print", "--dangerously-skip-permissions",
				"--allowedTools", "Bash",
				"--append-system-prompt", kilnSystemPrompt(),
				text,
			}
		},
	},
	"pi": {
		Name:    "pi",
		Display: "pi -p --provider zai --model glm-5.1 --tools bash --skill <kiln SKILL.md>  (Pi coding agent — kiln contract + skill, runs in /tmp/kiln-pi/)",
		Dir:     filepath.Join(os.TempDir(), "kiln-pi"),
		Detect:  func() bool { _, err := exec.LookPath("pi"); return err == nil },
		BuildArgs: func(text string) []string {
			argv := []string{"pi", "-p", "--provider", "zai", "--model", "glm-5.1",
				// Restrict pi to bash only — it dispatches kiln tools
				// via curl. The kiln world is the only source of truth
				// and is reachable solely via $KILN_URL HTTP — bash is
				// sufficient. Without this, pi's Read tool reaches for
				// cwd files instead of the world IR.
				"--tools", "bash"}
			// --skill loads SKILL.md (the tool surface). The contract
			// preamble ("you are inside kiln, no files exist") rides at
			// the head of the user prompt because pi has no separate
			// system-prompt flag. Together: skill = how to call tools,
			// contract = that calling tools is the only legal action.
			if p := kilnSkillPath(); p != "" {
				argv = append(argv, "--skill", p)
			}
			argv = append(argv, "<kiln-system>\n"+kilnContractPreamble+"\n</kiln-system>\n\n"+text)
			return argv
		},
	},
	"codex": {
		Name:    "codex",
		Display: "codex exec  (OpenAI Codex CLI; kiln contract + skill prepended to prompt)",
		Dir:     filepath.Join(os.TempDir(), "kiln-codex"),
		Detect:  func() bool { _, err := exec.LookPath("codex"); return err == nil },
		BuildArgs: func(text string) []string {
			// Codex exposes no --skill or --system-prompt flag, so the
			// kiln contract + skill ride at the head of the user prompt.
			// Same content as claude's --append-system-prompt; the
			// effective system prompt just lives in user-message position.
			prompt := "<kiln-system>\n" + kilnSystemPrompt() + "\n</kiln-system>\n\n" + text
			return []string{"codex", "exec", prompt}
		},
	},
}

// adapterAutoOrder is the priority list for `--agent auto` detection.
// Claude Code first because its credentials path is the most stable;
// pi second; codex last (less common locally).
var adapterAutoOrder = []string{"claude-code", "pi", "codex"}

// resolveAdapter maps a --agent flag value to a concrete Adapter.
//
//	""        → none (no agent runs)
//	"none"    → none (explicit)
//	"auto"    → first installed adapter from adapterAutoOrder, or none
//	"<name>"  → registry lookup; returns ok=false if not installed
//	"cmd …"   → custom freeform command; the string is split on whitespace
//	            and the user's prompt is appended as the final argument
//
// Returns (Adapter, ok). ok=false means "no agent will run."
func resolveAdapter(value string) (Adapter, bool) {
	switch value {
	case "", "none":
		return Adapter{}, false
	case "auto":
		for _, name := range adapterAutoOrder {
			if a := adapters[name]; a.Detect() {
				return a, true
			}
		}
		return Adapter{}, false
	}
	if a, ok := adapters[value]; ok {
		if !a.Detect() {
			return Adapter{}, false
		}
		return a, true
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return Adapter{}, false
	}
	// If the freeform spawn matches a built-in adapter's exact argv
	// prefix, return that named adapter — not a "custom" one. The gear
	// modal keys its "current" radio off the adapter name; classifying
	// "pi -p --provider zai --model glm-5.1" as custom would leave the
	// user staring at an all-unselected list even though their --agent
	// is the pi preset.
	for _, name := range adapterAutoOrder {
		a := adapters[name]
		argv := a.BuildArgs("")
		prefixArgs := argv[:len(argv)-1]
		if argvEqual(prefixArgs, parts) {
			return a, true
		}
	}
	binary := parts[0]
	prefix := append([]string(nil), parts...)
	return Adapter{
		Name:    "custom",
		Display: "custom: " + value,
		Detect: func() bool {
			_, err := exec.LookPath(binary)
			return err == nil
		},
		BuildArgs: func(text string) []string {
			return append(append([]string(nil), prefix...), text)
		},
	}, true
}

func argvEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
