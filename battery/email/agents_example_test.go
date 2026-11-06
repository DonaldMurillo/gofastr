package email

//gofastr:test-reads agents.md

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSMTPAgentExampleCompiles(t *testing.T) {
	document, err := os.ReadFile("agents.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(document), "**Shape (prod, SMTP):**")
	if !found {
		t.Fatal("SMTP example missing")
	}
	_, section, found = strings.Cut(section, "```go\n")
	if !found {
		t.Fatal("SMTP example fence missing")
	}
	snippet, _, found := strings.Cut(section, "```")
	if !found {
		t.Fatal("SMTP example fence unclosed")
	}
	source := "package sample\nimport \"github.com/DonaldMurillo/gofastr/battery/email\"\nfunc sample(u, p string) {\n" + snippet + "\n_ = s\n}\n"
	filename := filepath.Join(t.TempDir(), "sample_test.go")
	if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-run", "^$", filename)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("SMTP example does not compile: %v\n%s", err, output)
	}
}
