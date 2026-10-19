package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// Every Go file a fresh app generate writes carries an app scaffold
// marker at this CLI's version, and none reads as edited. The chrome
// fixture copies the canonical header and footer packages, so it covers
// styles output the app generator passes through too.
func TestGeneratedAppFilesAreMarked(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(chromeAppSpecYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runGenerate([]string{"app", bp}) })

	entries, err := scanProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]codegen.Marker{}
	for _, e := range entries {
		if e.Edited {
			t.Errorf("%s reads as edited straight after generate", e.Path)
		}
		marked[e.Path] = e.Marker
	}
	goFiles, genFiles := 0, 0
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		goFiles++
		rel, _ := filepath.Rel(dir, p)
		m, ok := marked[filepath.ToSlash(rel)]
		if !ok {
			t.Errorf("%s has no provenance marker", rel)
			return nil
		}
		// A copied chrome package's styles output stays derived from its
		// sheet; everything else is the app's scaffold.
		if strings.HasSuffix(p, ".gen.go") {
			genFiles++
			if m.Kind != codegen.KindDerived || m.Generator != "styles" {
				t.Errorf("%s marker = %#v, want derived styles", rel, m)
			}
		} else if m.Kind != codegen.KindScaffold || m.Generator != "app" || m.Version != markerVersion(cliVersion()) {
			t.Errorf("%s marker = %#v", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if goFiles == 0 || genFiles == 0 {
		t.Fatalf("generate wrote %d Go file(s), %d of them styles output; the fixture should cover both", goFiles, genFiles)
	}
}

// A marked file is judged by its own sum: one an older template wrote and
// nobody touched is not an edit, though today's output differs.
func TestForceSkipsUntouchedOldTemplate(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testAppSpecYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runGenerate([]string{"app", bp}) })

	appGo := filepath.Join(dir, "app.go")
	data, err := os.ReadFile(appGo)
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := codegen.ReadMarker("app.go", string(data))
	if !ok {
		t.Fatal("app.go has no marker")
	}
	old, err := codegen.Stamp("app.go", body+"\n// what an older template wrote\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appGo, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runGenerate([]string{"app", bp, "--force"}) })
	if strings.Contains(out, "edited since they were generated") {
		t.Fatalf("--force listed an untouched file:\n%s", out)
	}
}

// A scaffold marker names a release or says dev: a checkout build's
// pseudo-version would rewrite every scaffolded file on each commit.
func TestMarkerVersionIsReleaseOrDev(t *testing.T) {
	for v, want := range map[string]string{
		"v0.92.0":                               "v0.92.0",
		"v0.92.0-rc.1":                          "v0.92.0-rc.1",
		"dev":                                   "dev",
		"":                                      "dev",
		"v0.91.1-0.20261019184220-15b42c88f694": "dev",
		"v0.91.1-0.20261019184220-15b42c88f694+dirty": "dev",
		"v0.92.0+dirty": "dev",
	} {
		if got := markerVersion(v); got != want {
			t.Errorf("markerVersion(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestFileEdited(t *testing.T) {
	stamped, err := codegen.Stamp("a.go", "package a\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold})
	if err != nil {
		t.Fatal(err)
	}
	if fileEdited("a.go", stamped, "package b\n") {
		t.Error("untouched marked file judged edited against a different replacement")
	}
	if !fileEdited("a.go", stamped+"// x\n", stamped) {
		t.Error("edited marked file judged untouched")
	}
	if !fileEdited("a.go", "package a\n", "package b\n") {
		t.Error("unmarked file that differs judged untouched")
	}
	if fileEdited("a.go", "package a\n", "package a\n") {
		t.Error("unmarked identical file judged edited")
	}
}

// generate cli: a conflict says what made the files in the way, and
// --force names the ones edited since.
func TestGenerateCLIProvenance(t *testing.T) {
	dir := cliTempModule(t)
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })

	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code != 1 || !strings.Contains(out, "written by `gofastr generate cli "+markerVersion(cliVersion())+"`") {
		t.Fatalf("conflict: exit %d, output:\n%s", code, out)
	}

	mainGo := filepath.Join(dir, "cmd", "myapp", "main.go")
	data, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainGo, append(data, "\n// hand edit\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	out = covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp", "--force"}) })
	if !strings.Contains(out, "edited since they were generated") || !strings.Contains(out, filepath.Join("cmd", "myapp", "main.go")) {
		t.Fatalf("--force did not name the edited file:\n%s", out)
	}
	if strings.Contains(out, filepath.Join("cmd", "myapp", "auth.go")) {
		t.Fatalf("--force named an untouched file:\n%s", out)
	}
}

// generate sdk rebuilds its output; an edited file is named first.
func TestGenerateSDKWarnsEditedOutput(t *testing.T) {
	dir := cliTempModule(t)
	covT_capStdout(t, func() { runGenerateSDK(nil) })
	var clientGo string
	_ = filepath.WalkDir(filepath.Join(dir, "gen", "sdk"), func(p string, d os.DirEntry, err error) error {
		if err == nil && clientGo == "" && !d.IsDir() && strings.HasSuffix(p, ".go") {
			clientGo = p
		}
		return err
	})
	if clientGo == "" {
		t.Fatal("sdk wrote no Go file")
	}
	data, err := os.ReadFile(clientGo)
	if err != nil {
		t.Fatal(err)
	}
	if edited, ok := codegen.Edited(clientGo, string(data)); !ok || edited {
		t.Fatalf("fresh sdk file: edited=%v marked=%v", edited, ok)
	}
	if err := os.WriteFile(clientGo, append(data, "\n// hand edit\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runGenerateSDK(nil) })
	rel, _ := filepath.Rel(dir, clientGo)
	if !strings.Contains(out, rel+" was edited since `gofastr generate sdk` wrote it") {
		t.Fatalf("no edit warning for %s:\n%s", rel, out)
	}
	if again := covT_capStdout(t, func() { runGenerateSDK(nil) }); strings.Contains(again, "was edited since") {
		t.Fatalf("warned again after the rebuild:\n%s", again)
	}
}

func TestGenerateStylesWarnsEditedOutput(t *testing.T) {
	for _, tc := range []struct{ gen, sheet string }{
		{"issuecard_style.gen.go", "issuecard.style.css"},
		{"ui/acme_tokens.gen.go", "ui/acme.tokens.css"},
	} {
		t.Run(tc.gen, func(t *testing.T) {
			dir, code, printed := runStylesCase(t, map[string]string{
				"issuecard.style.css": "",
				"ui.go":               "",
				"ui/acme.tokens.css":  acmeTokensFixture,
				"ui/ui.go":            "package ui\n",
			}, "")
			if code != -1 && code != 0 {
				t.Fatalf("exit %d:\n%s", code, printed)
			}
			gen := filepath.Join(dir, filepath.FromSlash(tc.gen))
			data, err := os.ReadFile(gen)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(gen, append(data, "\n// hand edit\n"...), 0o600); err != nil {
				t.Fatal(err)
			}
			out := covT_capStdout(t, func() { runGenerateStyles(nil) })
			if !strings.Contains(out, tc.gen+" was edited since `gofastr generate styles` wrote it") ||
				!strings.Contains(out, "Make the change in "+tc.sheet) {
				t.Fatalf("no edit warning naming the sheet:\n%s", out)
			}
		})
	}
}

func TestGenerateConfigWarnsEditedOutput(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	files := codegen.NewFileSet()
	for _, p := range []string{"kept.txt", "edited.txt"} {
		if err := files.Add(codegen.GeneratedFile{Path: p, Content: "x\n"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := codegen.WriteFiles(files, codegen.WriteOptions{OutputRoot: "gen"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("gen", "edited.txt"), []byte("y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() {
		warnEditedOutputs(codegen.CodegenConfig{Output: "gen"}, files.All())
	})
	if !strings.Contains(out, filepath.Join("gen", "edited.txt")+" was edited since") || strings.Contains(out, "kept.txt") {
		t.Fatalf("output:\n%s", out)
	}
	clean := false
	out = covT_capStdout(t, func() {
		warnEditedOutputs(codegen.CodegenConfig{Output: "gen", Clean: &clean}, nil)
	})
	if out != "" {
		t.Fatalf("unclean run that does not rewrite the file warned:\n%s", out)
	}
}

func TestReportProvenance(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string, m codegen.Marker) {
		t.Helper()
		stamped, err := codegen.Stamp(rel, content, m)
		if err != nil {
			t.Fatal(err)
		}
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(stamped), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("app.go", "package main\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "v0.92.0"})
	write("ui/a_style.gen.go", "package ui\n", codegen.Marker{Generator: "styles", Kind: codegen.KindDerived})
	write("testdata/skip.go", "package skip\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold})
	for _, rel := range []string{"app.go", "ui/a_style.gen.go"} {
		f, err := os.OpenFile(filepath.Join(dir, rel), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("// edit\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	var notes, warns []string
	reportProvenance(dir,
		func(f string, a ...any) { notes = append(notes, strings.TrimSpace(fmt.Sprintf(f, a...))) },
		func(f string, a ...any) { warns = append(warns, strings.TrimSpace(fmt.Sprintf(f, a...))) })
	if len(notes) != 1 || notes[0] != "Generated files: derived styles: 1, scaffold app v0.92.0: 1; 1 scaffold file(s) edited since, which is expected: they are yours" {
		t.Fatalf("notes = %q", notes)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "ui/a_style.gen.go was edited since `gofastr generate styles`") {
		t.Fatalf("warns = %q", warns)
	}
}

// The full config path: a second plain generate names the extension
// output edited since the first.
func TestGenerateConfigRunWarnsEdited(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.MkdirAll("tools", 0o700); err != nil {
		t.Fatal(err)
	}
	ext := "#!/bin/sh\ninput=$(cat)\ncase \"$input\" in\n*'\"phase\":\"render\"'*) printf '{\"protocol_version\":1,\"files\":[{\"path\":\"out.txt\",\"content\":\"x\\\\n\"}]}' ;;\n*) printf '{\"protocol_version\":1}' ;;\nesac\n"
	//gofastr:allow(worldreadable) the fixture extension must be executable to run
	if err := os.WriteFile(filepath.Join("tools", "ext.sh"), []byte(ext), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("input.json", []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCodegenConfig(t, dir, "    - name: custom/report\n      extension: report\n      source:\n        type: json_file\n        path: input.json\n  extensions:\n    - name: report\n      command: [\"./tools/ext.sh\"]\n")
	if out := covT_capStdout(t, func() { covT_capExit(t, func() { generateProject([]string{"--config=gofastr.codegen.yml"}) }) }); !strings.Contains(out, "Generated 1 file(s)") {
		t.Fatalf("first run:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join("gen", "out.txt"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { covT_capExit(t, func() { generateProject([]string{"--config=gofastr.codegen.yml"}) }) })
	if !strings.Contains(out, filepath.Join("gen", "out.txt")+" was edited since the last `gofastr generate`") {
		t.Fatalf("second run did not warn:\n%s", out)
	}
}
