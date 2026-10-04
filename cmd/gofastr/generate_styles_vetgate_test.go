package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The emitted-code gate for gen styles, on the pattern of
// TestGeneratedCLIPassesRepoVettool: the file the generator writes is
// (1) produced by the committed generator (not the golden copy),
// (2) compiled and behaviour-tested in a temp module that resolves
// the repo through a replace directive, and (3) vetted whole by the
// repo vettool, so a finding anywhere in the emitted shape is a
// finding in the generator, not in the customer's code.
func TestGeneratedStylesPassRepoVettool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain to vet the generated module: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Generate with the committed generator into a fresh module.
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	css, err := os.ReadFile(filepath.Join(styleGenFixtureDir, "issuecard.style.css"))
	if err != nil {
		t.Fatal(err)
	}
	styleGenSeed(t, dir, map[string]string{
		"ui/issuecard.style.css": string(css),
		"ui/ui.go":               "package ui\n\n// Hello is here so the directory is a Go package the generated file joins.\nfunc Hello() string { return \"hi\" }\n",
		"ui/acme.tokens.css":     acmeTokensFixture,
		"hero/hero.go":           "package hero\n",
		"hero/hero.style.css":    ":scope { padding-block: var(--size-hero-gap); color: var(--color-highlight); font-weight: var(--font-weight-display); }\n",
	})
	covT_chdir(t, dir)
	covT_capStdout(t, func() {
		if code := covT_capExit(t, func() { runGenerateStyles(nil) }); code != -1 && code != 0 {
			t.Fatalf("generator exited %d", code)
		}
	})
	for _, name := range []string{"ui/issuecard_style.gen.go", "ui/acme_tokens.gen.go", "hero/hero_style.gen.go", ".gofastr/tokens.css"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s not written: %v", name, err)
		}
	}

	// The behaviour half: exercise the generated vocabulary exactly as
	// a caller would (this is the test the brief's behaviour contract
	// names: ColumnWith, ParsePriority, Root/RootWith, Scope).
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "ui", "generated_behavior_test.go"), `package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestGeneratedStyleBehavior(t *testing.T) {
	if got := Style.ColumnWith(ColumnVariants{OverLimit: true}); got != "column over-limit" {
		t.Errorf("ColumnWith(over-limit) = %q, want %q", got, "column over-limit")
	}
	if got := Style.Column(); got != "column" {
		t.Errorf("Column() = %q", got)
	}
	if p, ok := ParsePriority("urgent"); !ok || p != PriorityUrgent || string(p) != "priority--urgent" {
		t.Errorf("ParsePriority(urgent) = %q, %v", p, ok)
	}
	if p, ok := ParsePriority("unknown"); ok || p != "" {
		t.Errorf("ParsePriority(unknown) = %q, %v, want empty false", p, ok)
	}
	if got := Style.RootWith(RootVariants{Fresh: true, Priority: PriorityLow}); got != "fresh priority--low" {
		t.Errorf("RootWith = %q", got)
	}
	if got := Style.Root(); got != "" {
		t.Errorf("Root() = %q, want empty base", got)
	}
	if got := Style.Key(); got != "key" {
		t.Errorf("Key() = %q", got)
	}
	if Style.Name() != "issuecard" || Style.Kind() != ownstyle.KindScoped {
		t.Errorf("handle surface: %s %v", Style.Name(), Style.Kind())
	}
	entry, ok := registry.Lookup("issuecard")
	if !ok || entry.CSSFor(style.DefaultTheme()) == "" {
		t.Error("the sheet is not registered with compiled CSS")
	}
	th := style.DefaultTheme()
	th.DarkColors = map[string]string{"primary": "#818CF8", "primary-fg": "#1E1B4B"}
	th = th.Extend(Tokens)
	if err := th.Validate(); err != nil {
		t.Fatalf("theme with the app tokens: %v", err)
	}
	root := th.CSSCustomProperties()
	for _, want := range []string{"--size-hero-gap: clamp(2rem, 6vw, 5rem);", "--color-highlight: #0F766E;", "--font-weight-display: 800;", "--duration-unroll: 260ms;"} {
		if !strings.Contains(root, want) {
			t.Errorf("theme CSS is missing %q", want)
		}
	}
	if th.DarkColors["highlight"] != "#5EEAD4" {
		t.Errorf("dark value not merged: %v", th.DarkColors)
	}
	if got := Tokens.Sizes.HeroGap.CSS(); got != "var(--size-hero-gap)" {
		t.Errorf("Tokens.Sizes.HeroGap.CSS() = %q", got)
	}
	scoped := Style.Scope(render.HTML("<div class=\"wrap\">"))
	if !strings.Contains(string(scoped), "data-cui-scope=") || !strings.Contains(string(scoped), "issuecard") {
		t.Errorf("Scope did not stamp the marker: %s", scoped)
	}
}
`)

	// Build + behaviour.
	if out, err := exec.Command("go", "test", "./...").CombinedOutput(); err != nil {
		t.Fatalf("generated style package does not build or behave:\n%s", out)
	}

	// The vettool leg.
	vettool := filepath.Join(t.TempDir(), "vettool")
	build := exec.Command("go", "build", "-o", vettool, "./cmd/vettool")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the repo vettool failed: %v\n%s", err, out)
	}
	vet := exec.Command("go", "vet", "-vettool="+vettool, "./...")
	vet.Dir = dir
	vet.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := vet.CombinedOutput()
	if err != nil {
		text := string(out)
		for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
			if strings.Contains(text, marker) {
				t.Skipf("generated module could not be built in this environment: %s", text)
			}
		}
		t.Errorf("GATE: generated styles do not pass the repo vettool — every diagnostic is a finding in the emitter (core-ui/ownstyle/gen.go), not in the customer's code:\n%s", text)
	}
}

// acmeTokensFixture is a tokens file using every shape the generator
// emits: a string-valued, an int-valued and a duration token, a doc
// comment, and a dark colour.
const acmeTokensFixture = `/* Space above and below the home hero. */
@property --size-hero-gap { syntax: "<length>"; inherits: true; initial-value: clamp(2rem, 6vw, 5rem); }
@property --color-highlight { syntax: "<color>"; inherits: true; initial-value: #0F766E; }
@property --font-weight-display { syntax: "<number>"; inherits: true; initial-value: 800; }
@property --duration-unroll { syntax: "<time>"; inherits: true; initial-value: 260ms; }

@media (--dark) {
  :root { --color-highlight: #5EEAD4; }
}
`
