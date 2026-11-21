package webhook

//gofastr:test-reads agents.md

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebhookAgentExampleCompiles(t *testing.T) {
	document, err := os.ReadFile("agents.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(document), "**Shape:**")
	if !found {
		t.Fatal("Shape example missing")
	}
	_, section, found = strings.Cut(section, "```go\n")
	if !found {
		t.Fatal("Shape example fence missing")
	}
	snippet, _, found := strings.Cut(section, "```")
	if !found {
		t.Fatal("Shape example fence unclosed")
	}
	source := "package sample\n" +
		"import (\n" +
		"\"context\"\n" +
		"\"database/sql\"\n" +
		"\"time\"\n" +
		"\"github.com/DonaldMurillo/gofastr/battery/webhook\"\n" +
		"\"github.com/DonaldMurillo/gofastr/framework/event\"\n" +
		")\n" +
		"type sampleApp struct{}\n" +
		"func (sampleApp) Events() *event.EventBus { return nil }\n" +
		"func sample(db *sql.DB, ctx context.Context, payload []byte, key []byte, app sampleApp) error {\n" +
		snippet + "\n" +
		"return nil\n" +
		"}\n"
	filename := filepath.Join(t.TempDir(), "sample_test.go")
	if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "test", "-run", "^$", filename)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Shape example does not compile: %v\n%s", err, output)
	}
}
