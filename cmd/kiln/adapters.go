package main

import (
	"os/exec"
	"strings"
)

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
}

// adapters is the built-in registry. Adding a new agent is a one-entry
// change here.
var adapters = map[string]Adapter{
	"claude-code": {
		Name:    "claude-code",
		Display: "claude --print  (Claude Code, reads ~/.claude/.credentials.json)",
		Detect:  func() bool { _, err := exec.LookPath("claude"); return err == nil },
		BuildArgs: func(text string) []string {
			return []string{"claude", "--print", text}
		},
	},
	"pi": {
		Name:    "pi",
		Display: "pi -p --provider zai --model glm-5.1  (Pi coding agent)",
		Detect:  func() bool { _, err := exec.LookPath("pi"); return err == nil },
		BuildArgs: func(text string) []string {
			return []string{"pi", "-p", "--provider", "zai", "--model", "glm-5.1", text}
		},
	},
	"codex": {
		Name:    "codex",
		Display: "codex exec  (OpenAI Codex CLI)",
		Detect:  func() bool { _, err := exec.LookPath("codex"); return err == nil },
		BuildArgs: func(text string) []string {
			return []string{"codex", "exec", text}
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
