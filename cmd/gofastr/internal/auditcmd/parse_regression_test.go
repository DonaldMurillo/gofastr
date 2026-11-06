package auditcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditMalformedFilesHaveOneParseFinding(t *testing.T) {
	for _, name := range []string{"broken.go", "broken_test.go"} {
		for _, source := range []string{
			"package app\nfunc broken( {\n",
			"package app\nfunc check() { db.Exec(\"DELETE FROM sessions\"); app.Entity(\"members\", framework.EntityConfig{}) }\nfunc broken( {\nfunc alsoBroken( {\n",
		} {
			findings := lintRulesNamed(t, name, source)
			if len(findings) != 1 || findings[0].Rule != "parse-error" || findings[0].File != name || findings[0].Line <= 0 || !strings.Contains(findings[0].Message, name) {
				t.Errorf("%s findings=%+v, want exactly one file-scoped parse failure", name, findings)
			}
		}
	}
}

func TestAuditCLIMalformedFilesAreNotClean(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gofastr")
	if output, err := exec.Command("go", "build", "-o", binary, "github.com/DonaldMurillo/gofastr/cmd/gofastr").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, name := range []string{"broken.go", "broken_test.go"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, name), []byte("package app\nfunc broken( {\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "audit", "lint", root)
		output, err := command.CombinedOutput()
		if err == nil || command.ProcessState.ExitCode() != 1 || strings.Count(string(output), "parse-error") != 1 || strings.Contains(string(output), "No findings") {
			t.Errorf("%s code=%d err=%v\n%s", name, command.ProcessState.ExitCode(), err, output)
		}
	}
}
