package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The gen-styles fixtures. issuecard.style.css is the full-vocabulary
// sheet (plain class, base with flag, value group, :scope root flag,
// doc comments); issuecard_style.gen.go.golden is the byte-exact
// output the generator must reproduce.
const styleGenFixtureDir = "testdata/stylegen"

// styleGenSeed writes files into dir; an empty body means "seed from
// the committed fixture".
func styleGenSeed(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if body == "" {
			b, err := os.ReadFile(filepath.Join(styleGenFixtureDir, name))
			if err != nil {
				t.Fatal(err)
			}
			body = string(b)
		}
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runStylesCase seeds dir, runs `generate styles` there, and returns
// the exit code and captured output. wantNoFile names a file the
// failed run must NOT have written.
func runStylesCase(t *testing.T, files map[string]string, wantNoFile string) (dir string, code int, printed string) {
	t.Helper()
	dir = t.TempDir()
	styleGenSeed(t, dir, files)
	covT_chdir(t, dir)
	printed = covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runGenerateStyles(nil) })
	})
	if wantNoFile != "" {
		if _, statErr := os.Stat(filepath.Join(dir, wantNoFile)); !os.IsNotExist(statErr) {
			t.Errorf("failed run still wrote %s", wantNoFile)
		}
	}
	return dir, code, printed
}

// TestGenerateStylesGolden pins the generated file byte for byte: the
// same CSS must always produce the same bytes (headers, order, docs).
func TestGenerateStylesGolden(t *testing.T) {
	// The golden is read before the run: the run chdirs into its
	// temp tree, and the fixture path is package-relative.
	want, err := os.ReadFile(filepath.Join(styleGenFixtureDir, "issuecard_style.gen.go.golden"))
	if err != nil {
		t.Fatal(err)
	}
	dir, code, _ := runStylesCase(t, map[string]string{
		"issuecard.style.css": "",
		"ui.go":               "",
	}, "")
	if code != -1 && code != 0 {
		t.Fatalf("exit %d, want success (no exit)", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "issuecard_style.gen.go"))
	if err != nil {
		t.Fatalf("generated file not written: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated file drifted from the golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestGenerateStylesDeterministic proves the determinism the golden
// and GOFASTR1814 both stand on: run twice over the same tree, the
// bytes are identical.
func TestGenerateStylesDeterministic(t *testing.T) {
	dir := t.TempDir()
	styleGenSeed(t, dir, map[string]string{
		"issuecard.style.css": "",
		"ui.go":               "",
	})
	covT_chdir(t, dir)
	for i := range 2 {
		if code := covT_capExit(t, func() { runGenerateStyles(nil) }); code != -1 && code != 0 {
			t.Fatalf("run %d exit %d", i, code)
		}
	}
	first, err := os.ReadFile(filepath.Join(dir, "issuecard_style.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, dir) // ReadFile above is absolute-safe; re-run needs the cwd
	if code := covT_capExit(t, func() { runGenerateStyles(nil) }); code != -1 && code != 0 {
		t.Fatalf("third run exit %d", code)
	}
	second, err := os.ReadFile(filepath.Join(dir, "issuecard_style.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two runs over identical CSS produced different bytes")
	}
}

// A package holding more than one style file exports each handle as
// <Owner>Style instead of Style, and the handle types derive from the
// owner so the two files coexist in one package.
func TestGenerateStylesSharedPackageVarNames(t *testing.T) {
	dir, code, _ := runStylesCase(t, map[string]string{
		"issuecard.style.css": "",
		"ui.go":               "",
		"board.style.css": ":scope { display: grid; gap: var(--spacing-lg); }\n" +
			".column { display: grid; gap: var(--spacing-sm); }\n" +
			".column.over-limit .count { color: var(--color-warning); }\n" +
			".count { font-size: var(--text-xs); }\n",
	}, "")
	if code != -1 && code != 0 {
		t.Fatalf("exit %d, want success (no exit)", code)
	}
	for _, tc := range []struct{ file, want string }{
		{"issuecard_style.gen.go", `var IssuecardStyle = issuecardStyle{ownstyle.Must("issuecard", ownstyle.KindScoped, issuecardCSS)}`},
		{"board_style.gen.go", `var BoardStyle = boardStyle{ownstyle.Must("board", ownstyle.KindScoped, boardCSS)}`},
	} {
		body, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if !strings.Contains(string(body), tc.want) {
			t.Errorf("%s does not declare %s", tc.file, tc.want)
		}
	}
}

// Error cases: each must write nothing for the failing file, print a
// message naming it, and exit non-zero.
func TestGenerateStylesErrorCases(t *testing.T) {
	goodCSS := ":scope { display: grid; gap: var(--spacing-xs); }\n.key { color: var(--color-text-muted); }\n"

	assertFailed := func(t *testing.T, code int, printed, wantPrint string) {
		t.Helper()
		if code != 1 {
			t.Errorf("exit %d, want 1", code)
		}
		if !strings.Contains(printed, wantPrint) {
			t.Errorf("output does not contain %q:\n%s", wantPrint, printed)
		}
	}

	t.Run("error diagnostic", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"bad.style.css": ".card { border-radius: var(--radii-zz); }\n",
			"ui.go":         "",
		}, "bad_style.gen.go")
		assertFailed(t, code, printed, "bad.style.css:1:28: error GOFASTR1806")
	})

	t.Run("identifier collision", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"coll.style.css": ".a-b { color: var(--color-text); }\n.a_b { color: var(--color-border); }\n",
			"ui.go":          "",
		}, "coll_style.gen.go")
		assertFailed(t, code, printed, "both become the method")
	})

	t.Run("class shadows Sheet method", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"sh.style.css": ".scope { color: var(--color-text); }\n",
			"ui.go":        "",
		}, "sh_style.gen.go")
		assertFailed(t, code, printed, "the embedded *ownstyle.Sheet's Scope method")
	})

	t.Run("duplicate owner names", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"a/dup.style.css": goodCSS,
			"a/a.go":          "package a\n",
			"b/dup.style.css": goodCSS,
			"b/b.go":          "package b\n",
		}, "a/dup_style.gen.go")
		assertFailed(t, code, printed, `an owned style named "dup" already exists`)
	})

	t.Run("invalid file name", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"ui-card.style.css": goodCSS,
			"ui.go":             "",
		}, "")
		assertFailed(t, code, printed, `"ui-card" is not a valid owned style name`)
	})

	t.Run("directory with no Go package", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"loose.style.css": goodCSS,
		}, "loose_style.gen.go")
		assertFailed(t, code, printed, "holds a style sheet but no non-test Go files")
	})

	// One bad file fails the run at the END, not in the middle: the
	// good sibling is still generated, so a single run reports every
	// problem.
	t.Run("one bad file does not stop the others", func(t *testing.T) {
		dir, code, printed := runStylesCase(t, map[string]string{
			"bad.style.css":  ".card { border-radius: var(--radii-zz); }\n",
			"good.style.css": goodCSS,
			"ui.go":          "",
		}, "bad_style.gen.go")
		assertFailed(t, code, printed, "bad.style.css:1:28: error GOFASTR1806")
		if _, err := os.Stat(filepath.Join(dir, "good_style.gen.go")); err != nil {
			t.Errorf("good file not written while the bad one failed: %v", err)
		}
	})
}

// App tokens: a tokens file generates typed Go, its tokens are known to
// every owned sheet, its mistakes block only its own Go, and an allow
// marker is the one way past GOFASTR1806.
func TestGenerateStylesTokensFiles(t *testing.T) {
	t.Run("generates and shares its tokens", func(t *testing.T) {
		dir, code, printed := runStylesCase(t, map[string]string{
			"ui/acme.tokens.css": acmeTokensFixture,
			"ui/hero.style.css":  ":scope { padding-block: var(--size-hero-gap); }\n",
			"ui/ui.go":           "package ui\n",
		}, "")
		if code != -1 && code != 0 {
			t.Fatalf("exit %d:\n%s", code, printed)
		}
		gen, err := os.ReadFile(filepath.Join(dir, "ui", "acme_tokens.gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(gen), "var Tokens = acmeTokens{") {
			t.Errorf("generated tokens file:\n%s", gen)
		}
		completion, _ := os.ReadFile(filepath.Join(dir, ".gofastr", "tokens.css"))
		if !strings.Contains(string(completion), "--color-highlight: #0F766E; /* dark: #5EEAD4 */") {
			t.Errorf("completion file is missing the app token:\n%s", completion)
		}
	})

	t.Run("a bad tokens file writes no Go", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"ui/acme.tokens.css": "@property --size-x { syntax: \"<color>\"; inherits: true; initial-value: red; }\n",
			"ui/ui.go":           "package ui\n",
		}, "ui/acme_tokens.gen.go")
		if code != 1 || !strings.Contains(printed, `ui/acme.tokens.css:1:11: error tokens-file --size-x is a Size token`) {
			t.Errorf("exit %d:\n%s", code, printed)
		}
	})

	t.Run("a built-in name is refused", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"ui/acme.tokens.css": "@property --color-primary { syntax: \"<color>\"; inherits: true; initial-value: #000; }\n",
			"ui/ui.go":           "package ui\n",
		}, "ui/acme_tokens.gen.go")
		if code != 1 || !strings.Contains(printed, "--color-primary is a built-in theme token") {
			t.Errorf("exit %d:\n%s", code, printed)
		}
	})

	t.Run("a value another token holds is GOFASTR1821", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"ui/acme.tokens.css": "@property --color-brand { syntax: \"<color>\"; inherits: true; initial-value: #4F46E5; }\n",
			"ui/ui.go":           "package ui\n",
		}, "ui/acme_tokens.gen.go")
		if code != 1 || !strings.Contains(printed, "GOFASTR1821 --color-brand has the value of --color-primary") {
			t.Errorf("exit %d:\n%s", code, printed)
		}
	})

	t.Run("an allow marker waives GOFASTR1806", func(t *testing.T) {
		dir, code, printed := runStylesCase(t, map[string]string{
			"ui/embed.style.css": ":scope {\n  /* gofastr:allow(GOFASTR1806) the embedding page sets --host-ink */\n  color: var(--host-ink);\n}\n",
			"ui/ui.go":           "package ui\n",
		}, "")
		if code != -1 && code != 0 {
			t.Fatalf("exit %d:\n%s", code, printed)
		}
		if _, err := os.Stat(filepath.Join(dir, "ui", "embed_style.gen.go")); err != nil {
			t.Errorf("the waived sheet generated no Go: %v", err)
		}
	})

	t.Run("a literal in two sheets is a GOFASTR1822 warning", func(t *testing.T) {
		_, code, printed := runStylesCase(t, map[string]string{
			"ui/a.style.css": ":scope { max-width: 37rem; }\n",
			"ui/b.style.css": ":scope { width: 37rem; }\n",
			"ui/ui.go":       "package ui\n",
		}, "")
		if code != -1 && code != 0 {
			t.Fatalf("a warning failed the run: exit %d:\n%s", code, printed)
		}
		if !strings.Contains(printed, "ui/a.style.css:1:21: warn GOFASTR1822 37rem is also written in ui/b.style.css") {
			t.Errorf("output:\n%s", printed)
		}
	})
}

// tokens.css carries every theme token, light value first, dark value
// in a comment, and is refreshed on every run.
func TestGenerateStylesWritesTokensCSS(t *testing.T) {
	dir, code, _ := runStylesCase(t, map[string]string{
		"issuecard.style.css": "",
		"ui.go":               "",
	}, "")
	if code != -1 && code != 0 {
		t.Fatalf("exit %d, want success (no exit)", code)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gofastr", "tokens.css"))
	if err != nil {
		t.Fatalf("tokens.css not written: %v", err)
	}
	css := string(body)
	for _, want := range []string{
		":root {",
		"--color-primary: #4F46E5; /* dark: #A5B4FC */",
		"--spacing-md: 8px;",
		"--breakpoint-md: 768px;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("tokens.css missing %q", want)
		}
	}
}
