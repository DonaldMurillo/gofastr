package generatecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// A marked file is judged by its own sum: one an older template wrote and
// nobody touched is not an edit, though today's output differs.
func TestForceSkipsUntouchedOldTemplate(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.TestAppSpecYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { RunGenerate([]string{"app", bp}) })

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
	out := clitest.CovT_capStdout(t, func() { RunGenerate([]string{"app", bp, "--force"}) })
	if strings.Contains(out, "edited since they were generated") {
		t.Fatalf("--force listed an untouched file:\n%s", out)
	}
}

// generate cli rebuilds its derived files in place; an edited one is named
// before its edit is dropped, and an untouched one is not.
func TestGenerateCLIProvenance(t *testing.T) {
	dir := cliTempModule(t)
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })

	mainGo := filepath.Join(dir, "cmd", "myapp", "main.gen.go")
	data, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, ok := codegen.ReadMarker(mainGo, string(data)); !ok || m.Kind != codegen.KindDerived || m.Generator != "cli" {
		t.Fatalf("main.gen.go marker = %+v (marked %v), want cli derived", m, ok)
	}
	if err := os.WriteFile(mainGo, append(data, "\n// hand edit\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code > 0 {
		t.Fatalf("a second run must rebuild, not refuse: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, filepath.Join("cmd", "myapp", "main.gen.go")+" was edited since `gofastr generate cli` wrote it") {
		t.Fatalf("no edit warning for main.gen.go:\n%s", out)
	}
	if strings.Contains(out, "auth.gen.go was edited") {
		t.Fatalf("warned about an untouched file:\n%s", out)
	}
	if again, _ := os.ReadFile(mainGo); strings.Contains(string(again), "hand edit") {
		t.Fatal("the rebuild kept the edit in a derived file")
	}
}

// generate sdk rebuilds its output; an edited file is named first.
func TestGenerateSDKWarnsEditedOutput(t *testing.T) {
	dir := cliTempModule(t)
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateSDK(nil) })
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
	out := clitest.CovT_capStdout(t, func() { appgen.RunGenerateSDK(nil) })
	rel, _ := filepath.Rel(dir, clientGo)
	if !strings.Contains(out, rel+" was edited since `gofastr generate sdk` wrote it") {
		t.Fatalf("no edit warning for %s:\n%s", rel, out)
	}
	if again := clitest.CovT_capStdout(t, func() { appgen.RunGenerateSDK(nil) }); strings.Contains(again, "was edited since") {
		t.Fatalf("warned again after the rebuild:\n%s", again)
	}
}
