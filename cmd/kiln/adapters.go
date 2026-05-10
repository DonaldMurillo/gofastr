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
		Display: "claude --print --dangerously-skip-permissions  (Claude Code, reads ~/.claude/.credentials.json)",
		Detect:  func() bool { _, err := exec.LookPath("claude"); return err == nil },
		BuildArgs: func(text string) []string {
			// --dangerously-skip-permissions is required for non-interactive
			// runs — without it claude --print hangs at the first Bash/Edit
			// tool-use waiting for a permission prompt that nobody can answer.
			// Kiln invokes Claude in a controlled, kiln-scoped session, and
			// the only "tool" the agent uses is curl against $KILN_URL, so
			// bypassing the prompt is correct.
			return []string{"claude", "--print", "--dangerously-skip-permissions", text}
		},
	},
	"pi": {
		Name:    "pi",
		Display: "pi -p --provider zai --model glm-5.1 --tools bash --skill <kiln SKILL.md>  (Pi coding agent — runs in /tmp/kiln-pi/ to isolate from cwd)",
		Dir:     filepath.Join(os.TempDir(), "kiln-pi"),
		Detect:  func() bool { _, err := exec.LookPath("pi"); return err == nil },
		BuildArgs: func(text string) []string {
			argv := []string{"pi", "-p", "--provider", "zai", "--model", "glm-5.1",
				// Restrict pi to bash only — it dispatches kiln tools
				// via curl. Without --tools=Bash, pi's Read tool sees
				// the cwd's Go source (e.g. examples/blog/) and reports
				// on it as if it were the kiln world, since both look
				// like 'app code' to the model. The kiln world is the
				// only source of truth and is reachable solely via
				// $KILN_URL HTTP — bash is sufficient.
				"--tools", "bash"}
			// Pi doesn't auto-load ~/.claude/skills/ — point it at
			// the kiln skill explicitly so the agent knows about
			// add_entity / add_page / etc. Without this pi just
			// hallucinates Go code instead of calling the tool API.
			if p := kilnSkillPath(); p != "" {
				argv = append(argv, "--skill", p)
			}
			argv = append(argv, text)
			return argv
		},
	},
	"codex": {
		Name:    "codex",
		Display: "codex exec  (OpenAI Codex CLI; kiln skill prepended to prompt)",
		Detect:  func() bool { _, err := exec.LookPath("codex"); return err == nil },
		BuildArgs: func(text string) []string {
			// Codex has no --skill flag — prepend the skill content
			// to the prompt so the agent has the kiln tool API
			// in its context.
			prompt := text
			if p := kilnSkillPath(); p != "" {
				if buf, err := os.ReadFile(p); err == nil {
					prompt = "<kiln-skill>\n" + string(buf) + "\n</kiln-skill>\n\n" + text
				}
			}
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
