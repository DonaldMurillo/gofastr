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
		`data-cui-comp="ui-code-tabs"`,
		`data-hui-tabs=""`,
		`data-cui-signal-set="install:0"`,
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

func TestCodeTabsCSSLeavesPanelsToHiddenAndUnderlinesTheActiveTab(t *testing.T) {
	css := codeTabsCSS(style.Theme{})
	// The strip is ruled and scrolls rather than wraps, the active tab
	// is underlined. Panel visibility is headless.Tabs' hidden
	// attribute alone: a display rule on the panel would beat
	// hidden="until-found" and stop find-in-page reaching it.
	for _, want := range []string{
		`[data-cui-comp="ui-code-tabs"] .fui-code-tabs__nav {`,
		`flex-wrap: nowrap;`,
		`overflow-x: auto;`,
		`box-shadow: inset 0 calc(-1 * var(--stroke-thin, 1px)) 0 var(--color-border`,
		`[data-cui-comp="ui-code-tabs"] .fui-code-tabs__strip[data-active="1"] .fui-code-tabs__tab[data-cui-tab-index="1"]{color:var(--color-primary`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("codeTabsCSS lost the rule %q — the strip regressed:\n%s", want, css)
		}
	}
	if strings.Contains(css, "fui-code-tabs__panel { display") || strings.Contains(css, "fui-code-tabs__panel[data-cui-tab-index") {
		t.Errorf("codeTabsCSS sets a panel's display; the hidden attribute owns panel visibility:\n%s", css)
	}
}

func TestCodeTabsLabelNamesTablist(t *testing.T) {
	out := string(CodeTabs(CodeTabsConfig{Name: "install", Label: "Install snippets"},
		CodeSample{Label: "Go", Code: "go get x"}))
	i := strings.Index(out, `role="tablist"`)
	if i < 0 {
		t.Fatalf("no tablist:\n%s", out)
	}
	start := strings.LastIndex(out[:i], "<")
	tablist := out[start : i+strings.Index(out[i:], ">")+1]
	if !strings.Contains(tablist, `aria-label="Install snippets"`) {
		t.Errorf("tablist is not named by Label: %s", tablist)
	}
	if n := strings.Count(out, `aria-label="Install snippets"`); n != 1 {
		t.Errorf("Label rendered %d times, want once", n)
	}
}
