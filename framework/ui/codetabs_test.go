package ui

import (
	"strings"
	"testing"
)

func TestCodeTabsRendersTabPerSample(t *testing.T) {
	out := string(CodeTabs(CodeTabsConfig{Name: "install", Label: "Install"},
		CodeSample{Label: "Go", Language: "go", Code: "package main"},
		CodeSample{Label: "TypeScript", Language: "ts", Code: "const x = 1;"},
		CodeSample{Label: "curl", Language: "shell", Code: "curl -s https://x"},
	))
	for _, want := range []string{
		`data-fui-comp="ui-code-tabs"`,
		`data-fui-comp="tabs"`,
		`name="install"`,
		">Go</", ">TypeScript</", ">curl</",
		`aria-label="Install"`,
		"ui-code-block", // each panel is a real CodeBlock
	} {
		if !strings.Contains(out, want) {
			t.Errorf("CodeTabs missing %q\n%s", want, out)
		}
	}
	if got := strings.Count(out, "<details"); got != 3 {
		t.Errorf("want 3 tabs, got %d", got)
	}
}

func TestCodeTabsEscapesSource(t *testing.T) {
	out := string(CodeTabs(CodeTabsConfig{Name: "x"},
		CodeSample{Label: "JS", Language: "js", Code: `alert("<script>")`}))
	if strings.Contains(out, "<script>") {
		t.Fatal("code sample was not escaped")
	}
}

func TestCodeTabsPanics(t *testing.T) {
	for name, fn := range map[string]func(){
		"no name":    func() { CodeTabs(CodeTabsConfig{}, CodeSample{Label: "a", Code: "b"}) },
		"no samples": func() { CodeTabs(CodeTabsConfig{Name: "x"}) },
		"no code":    func() { CodeTabs(CodeTabsConfig{Name: "x"}, CodeSample{Label: "a"}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
}
