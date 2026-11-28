package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

const kilnContractPreamble = `You are running inside the Kiln runtime.

Kiln's world IR is the only source of truth for the app being built.
There is no on-disk codebase to read, edit, or compile. Any files you
see on the local filesystem are unrelated to this session and reading
or modifying them does NOTHING: the user is watching the live world,
not the disk.

The ONLY way to inspect or change the app is HTTP against $KILN_URL:

  - Read state:   curl -s "$KILN_URL/kiln/world"
  - Call a tool:  curl -s -X POST "$KILN_URL/kiln/tool/<name>" \
                       -H 'Content-Type: application/json' -d '<json args>'

Do NOT use Read, Write, Edit, or Glob/Grep. Do NOT cd, git, install
packages, or write source files. The Bash tool exists solely so you
can curl the kiln HTTP API. If you find yourself reaching for a file
tool, you are off-script: go back to curl.

The available tools are documented in the kiln skill. Read it before
acting.`

func kilnSystemPrompt() string {
	out := kilnContractPreamble
	if p := kilnSkillPath(); p != "" {
		if buf, err := os.ReadFile(p); err == nil {
			out += "\n\n<kiln-skill>\n" + string(buf) + "\n</kiln-skill>"
		}
	}
	return out
}

// Adapter spawns a third-party coding-agent CLI for one turn: BuildArgs
// returns the argv, the prompt last; Dir, when set, is the parent of the
// turn's working directory. Each CLI handles its own login.
type Adapter struct {
	Name      string
	Display   string
	Detect    func() bool
	BuildArgs func(text string) []string
	Dir       string
}

var adapters = map[string]Adapter{
	"omp": {
		Name:    "omp",
		Display: "omp -p --model glm-5.2 --tools bash  (Oh My Pi with GLM-5.2; kiln contract + skill, isolated cwd)",
		Dir:     filepath.Join(os.TempDir(), "kiln-omp"),
		Detect:  func() bool { _, err := exec.LookPath("omp"); return err == nil },
		BuildArgs: func(text string) []string {
			return []string{
				"omp", "-p",
				"--model", "glm-5.2",
				"--tools", "bash",
				"--append-system-prompt", kilnSystemPrompt(),
				"--auto-approve",
				"--no-session",
				endOfOptions, text,
			}
		},
	},
	"claude-code": {
		Name:    "claude-code",
		Display: "claude --print --tools Bash --append-system-prompt <kiln contract>  (Claude Code, reads ~/.claude/.credentials.json)",
		Dir:     filepath.Join(os.TempDir(), "kiln-claude"),
		Detect:  func() bool { _, err := exec.LookPath("claude"); return err == nil },
		BuildArgs: func(text string) []string {
			return []string{
				"claude", "--print", "--dangerously-skip-permissions",
				"--tools", "Bash",
				"--append-system-prompt", kilnSystemPrompt(),
				endOfOptions, text,
			}
		},
	},
	"pi": {
		Name:    "pi",
		Display: "pi -p --provider zai --model glm-5.1 --tools bash --skill <kiln SKILL.md>  (Pi coding agent: kiln contract + skill, runs in /tmp/kiln-pi/)",
		Dir:     filepath.Join(os.TempDir(), "kiln-pi"),
		Detect:  func() bool { _, err := exec.LookPath("pi"); return err == nil },
		BuildArgs: func(text string) []string {
			argv := []string{"pi", "-p", "--provider", "zai", "--model", "glm-5.1",
				"--tools", "bash"}
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
			prompt := "<kiln-system>\n" + kilnSystemPrompt() + "\n</kiln-system>\n\n" + text
			return []string{"codex", "exec", prompt}
		},
	},
}

const endOfOptions = "--"

func spawnPrefix(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	return trimEndOfOptions(argv[:len(argv)-1])
}

func trimEndOfOptions(argv []string) []string {
	if n := len(argv); n > 0 && argv[n-1] == endOfOptions {
		return argv[:n-1]
	}
	return argv
}

var adapterAutoOrder = []string{"omp", "claude-code", "pi", "codex"}

// resolveAdapter maps an --agent value to an adapter: "" and "none" to
// none, "auto" to the first installed in adapterAutoOrder, a registered name
// to that adapter when installed, and anything else to a custom command.
// ok is false when no agent will run.
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
	parts := trimEndOfOptions(strings.Fields(value))
	if len(parts) == 0 {
		return Adapter{}, false
	}
	for _, name := range adapterAutoOrder {
		a := adapters[name]
		if argvEqual(spawnPrefix(a.BuildArgs("")), parts) {
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
			return append(append([]string(nil), prefix...), endOfOptions, text)
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
