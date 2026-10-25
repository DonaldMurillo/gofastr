package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

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
		if got := MarkerVersion(v); got != want {
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
			out := clitest.CovT_capStdout(t, func() { runGenerateStyles(nil) })
			if !strings.Contains(out, tc.gen+" was edited since `gofastr generate styles` wrote it") ||
				!strings.Contains(out, "Make the change in "+tc.sheet) {
				t.Fatalf("no edit warning naming the sheet:\n%s", out)
			}
		})
	}
}

func TestGenerateConfigWarnsEditedOutput(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
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
	out := clitest.CovT_capStdout(t, func() {
		warnEditedOutputs(codegen.CodegenConfig{Output: "gen"}, files.All())
	})
	if !strings.Contains(out, filepath.Join("gen", "edited.txt")+" was edited since") || strings.Contains(out, "kept.txt") {
		t.Fatalf("output:\n%s", out)
	}
	clean := false
	out = clitest.CovT_capStdout(t, func() {
		warnEditedOutputs(codegen.CodegenConfig{Output: "gen", Clean: &clean}, nil)
	})
	if out != "" {
		t.Fatalf("unclean run that does not rewrite the file warned:\n%s", out)
	}
}

// The full config path: a second plain generate names the extension
// output edited since the first.
func TestGenerateConfigRunWarnsEdited(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
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
	if out := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, func() { GenerateProject([]string{"--config=gofastr.settings.yml"}) }) }); !strings.Contains(out, "Generated 1 file(s)") {
		t.Fatalf("first run:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join("gen", "out.txt"), []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, func() { GenerateProject([]string{"--config=gofastr.settings.yml"}) }) })
	if !strings.Contains(out, filepath.Join("gen", "out.txt")+" was edited since the last `gofastr generate`") {
		t.Fatalf("second run did not warn:\n%s", out)
	}
}
