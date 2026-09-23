package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCodeTabsRendersTabPerSample(t *testing.T) {
	out := string(CodeTabs(CodeTabsConfig{Name: "install", Label: "Install"},
		CodeSample{Label: "Go", Language: "go", Code: "package main"},
		CodeSample{Label: "TypeScript", Language: "ts", Code: "const x = 1;"},
		CodeSample{Label: "curl", Language: "shell", Code: "curl -s https://x"},
	))
	for _, want := range []string{
		`data-fui-comp="ui-code-tabs"`,
		`data-hui-tabs=""`,
		`data-fui-signal-set="install:0"`,
		">Go</", ">TypeScript</", ">curl</",
		`href="#install-panel-0"`,
		"ui-code-block", // each panel is a real CodeBlock
	} {
		if !strings.Contains(out, want) {
			t.Errorf("CodeTabs missing %q\n%s", want, out)
		}
	}
	if got := strings.Count(out, `role="tab"`); got != 3 {
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

func TestCodeTabsExtraAttrsOnRoot(t *testing.T) {
	h := CodeTabs(CodeTabsConfig{
		Name:       "install",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	}, CodeSample{Label: "Go", Code: "x"})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root div missing data-test:\n%s", root)
	}
}

func TestCodeTabsCSSHidesInactivePanelsAndUnderlinesTheActiveTab(t *testing.T) {
	css := codeTabsCSS(style.Theme{})
	// The retired tabs pattern's contract, carried onto the classes
	// headless.Tabs renders: every panel hidden except the active
	// index, the strip bordered, the active tab underlined. Without
	// the per-index pair all panels stack (review item 19).
	for _, want := range []string{
		`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__nav {`,
		`border-bottom: 1px solid var(--color-border`,
		`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__panel { display: none;`,
		`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="0"] .fui-code-tabs__panel[data-fui-tab-index="0"]{display:block}`,
		`[data-fui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="1"] .fui-code-tabs__tab[data-fui-tab-index="1"]{color:var(--color-primary`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("codeTabsCSS lost the rule %q — the strip or the panel visibility regressed:\n%s", want, css)
		}
	}
}
