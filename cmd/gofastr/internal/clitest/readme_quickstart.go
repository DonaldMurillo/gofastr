package clitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The app spec quickstart lives in the tutorial doc, not the README.
// The README points at the CLI and the tutorial carries the executable
// path. The gate follows the content: it extracts the tutorial's own
// fences and runs them.
const appSpecTutorialPath = "framework/docs/content/tutorial-app-spec.md"

const ReadmeAppSpecHeading = "## 1. App spec → running app"

// ReadmeSection returns the README text between `heading` and the next
// heading of the same or higher level.
func ReadmeSection(content, heading string) (string, error) {
	idx := strings.Index(content, heading)
	if idx < 0 {
		return "", fmt.Errorf("README anchor missing: heading %q not found — the executable quickstart gate is pinned to it; if the section was renamed, update readme_quickstart_test.go AND keep the quickstart runnable", heading)
	}
	rest := content[idx+len(heading):]
	end := len(rest)
	for _, next := range []string{"\n## ", "\n### "} {
		if i := strings.Index(rest, next); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end], nil
}

// FencedBlock returns the body of the first ```<lang> fence in section
// whose body contains mustContain.
func FencedBlock(section, lang, mustContain string) (string, error) {
	lines := strings.Split(section, "\n")
	var body []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inFence && trimmed == "```"+lang:
			inFence = true
			body = body[:0]
		case inFence && trimmed == "```":
			inFence = false
			block := strings.Join(body, "\n")
			if strings.Contains(block, mustContain) {
				return block, nil
			}
		case inFence:
			body = append(body, line)
		}
	}
	return "", fmt.Errorf("README anchor missing: no ```%s fence containing %q under %q — the quickstart must keep this block; README edits that drop or rename it break the executable-README gate", lang, mustContain, ReadmeAppSpecHeading)
}

func appSpecTutorialContent(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(RepoRootDir(t), appSpecTutorialPath))
	if err != nil {
		t.Fatalf("read %s: %v", appSpecTutorialPath, err)
	}
	return string(raw)
}

// ReadmeQuickstartBlocks extracts the tutorial's app spec quickstart:
// the gofastr.yml fence, the generate/dev command fence, and the
// separate mkdir/go-mod-init fence that names the module.
func ReadmeQuickstartBlocks(t *testing.T) (yamlBlock, bashBlock, initBlock string) {
	t.Helper()
	section, err := ReadmeSection(appSpecTutorialContent(t), ReadmeAppSpecHeading)
	if err != nil {
		t.Fatal(err)
	}
	yamlBlock, err = FencedBlock(section, "yaml", "# gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	bashBlock, err = FencedBlock(section, "bash", "gofastr generate app gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	initBlock, err = FencedBlock(section, "bash", "go mod init")
	if err != nil {
		t.Fatal(err)
	}
	return yamlBlock, bashBlock, initBlock
}

// QuickstartModule extracts <module> from the documented
// `go mod init <module>` line.
func QuickstartModule(t *testing.T, bashBlock string) string {
	t.Helper()
	for _, line := range strings.Split(bashBlock, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "go" && fields[1] == "mod" && fields[2] == "init" {
			return fields[3]
		}
	}
	t.Fatalf("README quickstart bash block lost its `go mod init <module>` line:\n%s", bashBlock)
	return ""
}
