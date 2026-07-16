package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveAdapter(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantOK    bool
		wantArgv0 string // first argv element from BuildArgs("hello"), or ""
	}{
		// Sentinels
		{"empty is none", "", false, ""},
		{"explicit none", "none", false, ""},

		// Custom command — always resolvable to a built adapter (whether
		// the binary is installed is the runtime concern).
		{"custom freeform cmd", "/bin/echo hi", true, "/bin/echo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := resolveAdapter(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if a.BuildArgs == nil {
				t.Fatal("BuildArgs is nil for resolved adapter")
			}
			argv := a.BuildArgs("hello")
			if len(argv) == 0 {
				t.Fatal("BuildArgs returned empty argv")
			}
			if argv[0] != tc.wantArgv0 {
				t.Errorf("argv[0] = %q, want %q", argv[0], tc.wantArgv0)
			}
			// Last element must be the prompt text we passed in.
			if argv[len(argv)-1] != "hello" {
				t.Errorf("last argv = %q, want %q", argv[len(argv)-1], "hello")
			}
		})
	}
}

func TestResolveAdapterCustomBuildArgs(t *testing.T) {
	a, ok := resolveAdapter("custombin --flag1 value1 --flag2")
	if !ok {
		t.Fatal("custom freeform should resolve")
	}
	want := []string{"custombin", "--flag1", "value1", "--flag2", "the prompt"}
	got := a.BuildArgs("the prompt")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgs = %#v, want %#v", got, want)
	}
}

func TestOMPAdapterPinsGLM52AndKilnBoundary(t *testing.T) {
	a := adapters["omp"]
	argv := a.BuildArgs("build the app")
	joined := strings.Join(argv, " ")
	for _, want := range []string{"omp", "--model glm-5.2", "--tools bash", "--append-system-prompt", "--auto-approve", "--no-session"} {
		if !strings.Contains(joined, want) {
			t.Errorf("OMP argv missing %q: %v", want, argv)
		}
	}
	if argv[len(argv)-1] != "build the app" {
		t.Errorf("last argv = %q, want user prompt", argv[len(argv)-1])
	}
	if !strings.Contains(joined, "$KILN_URL") {
		t.Errorf("OMP system prompt does not enforce the Kiln HTTP boundary: %v", argv)
	}
	if a.Dir == "" {
		t.Error("OMP adapter should isolate the spawned agent from Kiln's cwd")
	}
}

func TestAdapterAutoOrderPrefersOMP(t *testing.T) {
	if len(adapterAutoOrder) == 0 || adapterAutoOrder[0] != "omp" {
		t.Fatalf("adapterAutoOrder = %v, want omp first", adapterAutoOrder)
	}
}

// pi has no auto-discovery for ~/.claude/skills/ — its adapter must
// inject --skill <path> when the kiln skill file exists. Without it
// pi has no idea about the kiln tool API and just hallucinates Go
// code instead of calling add_entity / add_page / etc.
func TestPiAdapterIncludesSkillFlag(t *testing.T) {
	// Skip when the skill isn't installed (e.g. CI without ~/.claude).
	if kilnSkillPath() == "" {
		t.Skip("kiln skill not installed at ~/.claude/skills/kiln/SKILL.md")
	}
	a := adapters["pi"]
	argv := a.BuildArgs("hi")
	var sawSkill bool
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--skill" && strings.HasSuffix(argv[i+1], "SKILL.md") {
			sawSkill = true
			break
		}
	}
	if !sawSkill {
		t.Errorf("pi argv missing '--skill <kiln-skill-path>': %v", argv)
	}
}

// codex has no --skill flag, so the adapter prepends the kiln contract
// + skill to the prompt as a tagged block. The contract is what tells
// the model "files on disk are not the app — only the world IR is".
func TestCodexAdapterPrependsSkillToPrompt(t *testing.T) {
	if kilnSkillPath() == "" {
		t.Skip("kiln skill not installed at ~/.claude/skills/kiln/SKILL.md")
	}
	a := adapters["codex"]
	argv := a.BuildArgs("real prompt")
	if len(argv) < 3 {
		t.Fatalf("codex argv too short: %v", argv)
	}
	prompt := argv[len(argv)-1]
	if !strings.Contains(prompt, "<kiln-system>") {
		t.Errorf("codex prompt missing <kiln-system> contract block; got %q", prompt)
	}
	if !strings.Contains(prompt, "<kiln-skill>") {
		t.Errorf("codex prompt missing <kiln-skill> tool block; got %q", prompt)
	}
	if !strings.Contains(prompt, "real prompt") {
		t.Errorf("codex prompt dropped the original user text; got %q", prompt)
	}
	if a.Dir == "" {
		t.Error("codex adapter should set Dir to isolate from kiln's cwd")
	}
}

// claude-code on main was the worst-off adapter: it inherited kiln's
// cwd (a real Go repo when kiln serve runs in the gofastr tree) and got
// nothing but the raw user prompt, so claude --print fell back to
// Read/Edit/Write on local files instead of calling kiln HTTP tools.
// This test pins the three things that have to be true for the agent
// to behave like a kiln client:
//
//   - --allowedTools restricts the agent to Bash (curl is the only
//     thing it needs; Edit/Write/Read are not even available).
//   - --append-system-prompt installs the kiln contract at the system
//     level, where it outranks claude's default tool autonomy.
//   - Dir is set so the agent doesn't see a Go repo as cwd in the
//     first place.
func TestClaudeCodeAdapterEnforcesKilnContract(t *testing.T) {
	a := adapters["claude-code"]
	argv := a.BuildArgs("real prompt")
	if len(argv) == 0 || argv[0] != "claude" {
		t.Fatalf("claude argv malformed: %v", argv)
	}

	var sawAllowedTools, sawAppendSystem bool
	var systemPromptValue string
	for i := 0; i < len(argv)-1; i++ {
		switch argv[i] {
		case "--allowedTools":
			sawAllowedTools = true
			if argv[i+1] != "Bash" {
				t.Errorf("--allowedTools = %q, want %q (curl is dispatched via Bash; no other tool is needed)", argv[i+1], "Bash")
			}
		case "--append-system-prompt":
			sawAppendSystem = true
			systemPromptValue = argv[i+1]
		}
	}
	if !sawAllowedTools {
		t.Error("claude argv missing --allowedTools — claude can fall back to Read/Edit/Write without it")
	}
	if !sawAppendSystem {
		t.Error("claude argv missing --append-system-prompt — without the kiln contract at system level the model treats kiln as advisory")
	}
	// The contract must mention the HTTP boundary: any system prompt
	// that doesn't tell claude to curl $KILN_URL is just decoration.
	if sawAppendSystem && !strings.Contains(systemPromptValue, "$KILN_URL") {
		t.Errorf("--append-system-prompt missing $KILN_URL reference; got %q", systemPromptValue)
	}
	if a.Dir == "" {
		t.Error("claude-code adapter should set Dir to isolate from kiln's cwd")
	}
	// Last argv must still be the user prompt (regression pin).
	if argv[len(argv)-1] != "real prompt" {
		t.Errorf("last argv = %q, want %q (user prompt must remain trailing)", argv[len(argv)-1], "real prompt")
	}
}

// pi already had --skill + /tmp/kiln-pi/ + --tools bash. The remaining
// gap was the contract preamble — telling pi explicitly that files on
// disk don't matter — which now rides at the head of the user prompt.
func TestPiAdapterIncludesContractPreamble(t *testing.T) {
	a := adapters["pi"]
	argv := a.BuildArgs("real prompt")
	last := argv[len(argv)-1]
	if !strings.Contains(last, "<kiln-system>") {
		t.Errorf("pi prompt missing <kiln-system> contract block; got %q", last)
	}
	if !strings.Contains(last, "real prompt") {
		t.Errorf("pi prompt dropped the original user text; got %q", last)
	}
}

// The contract preamble itself must articulate the kiln model: world
// IR is the source of truth, $KILN_URL is the only mutation surface,
// disk tools are forbidden. This is what makes the difference between
// "agent edits files" and "agent calls kiln tools".
func TestKilnContractPreambleMentionsTheBoundary(t *testing.T) {
	must := []string{"$KILN_URL", "world", "Do NOT"}
	for _, needle := range must {
		if !strings.Contains(kilnContractPreamble, needle) {
			t.Errorf("kilnContractPreamble missing %q — agents need this to know the kiln contract", needle)
		}
	}
}

// When the user passes --agent as a freeform string that happens to
// equal a built-in adapter's exact spawn command, classify it as that
// named adapter — not "custom". Otherwise the gear modal can't mark a
// "current" radio (curName=="custom" matches none of the listed
// adapter rows), and the user sees an unselected list.
//
// Adapters whose canonical spawn argv contains a token with internal
// whitespace (e.g. claude-code's --append-system-prompt <multi-line
// contract>) cannot roundtrip through `strings.Fields`-based shell
// parsing, so they're skipped. The user is expected to use --agent
// claude-code (the named form) for those, which goes through direct
// registry lookup, not freeform parsing.
func TestResolveAdapterFreeformMatchesBuiltin(t *testing.T) {
	for name, want := range adapters {
		// Build the adapter's natural spawn command (argv minus the
		// trailing prompt) and feed it back through resolveAdapter as
		// a single string — the same path --agent "<freeform>" takes.
		argv := want.BuildArgs("")
		prefix := argv[:len(argv)-1]
		if anyTokenHasWhitespace(prefix) {
			t.Logf("%s: skipping freeform roundtrip (one or more argv tokens contain whitespace and can't survive shell-style splitting)", name)
			continue
		}
		spawn := joinArgv(prefix)
		got, ok := resolveAdapter(spawn)
		if !ok {
			t.Errorf("%s: spawn cmd %q failed to resolve", name, spawn)
			continue
		}
		if got.Name != name {
			t.Errorf("%s: spawn cmd %q resolved to adapter %q, want %q (so the gear modal can mark it current)",
				name, spawn, got.Name, name)
		}
	}
}

func anyTokenHasWhitespace(args []string) bool {
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n\r") {
			return true
		}
	}
	return false
}

func joinArgv(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func TestResolveAdapterUnknownName(t *testing.T) {
	// Names that aren't in the registry and aren't shell-cmd-shaped
	// (single token) should NOT resolve to "custom" — that would be
	// confusing. They go through the freeform path which then fails
	// the Detect check at runtime.
	a, ok := resolveAdapter("not-a-real-agent-name-xyz")
	if !ok {
		t.Skip("unknown bare name treated as a custom command name") // documenting current behavior
	}
	// If ok, Detect should return false (binary not on PATH).
	if a.Detect == nil {
		t.Fatal("Detect is nil")
	}
	if a.Detect() {
		t.Errorf("Detect on bogus name returned true")
	}
}
