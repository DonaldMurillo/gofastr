package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

func sweepBasicsPanics(fn func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	fn()
	return false
}

func TestSweepBasicsSheetsOnlyConsumeOptionVariables(t *testing.T) {
	th := style.DefaultTheme()
	decl := regexp.MustCompile(`(?:^|[;{\s])(--fui-[a-z0-9-]+)\s*:`)
	sheets := map[string]string{"buttonCSS": buttonCSS(th), "formFieldCSS": formFieldCSS(th)}
	for _, d := range componentOptionsCSS(theme.DefaultOptions.Flattened()) {
		for name, css := range sheets {
			for _, m := range decl.FindAllStringSubmatch(css, -1) {
				if m[1] == d.Name {
					t.Errorf("%s redeclares the option variable %s, which blocks its inheritance from the theme boundary", name, d.Name)
				}
			}
		}
	}
}

func TestSweepBasicsCodeBlockEmptyWordNeverMarks(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		done <- string(CodeBlock(CodeBlockConfig{Code: "ab", HighlightWords: []string{"", "b"}}))
	}()
	select {
	case out := <-done:
		if strings.Count(out, "<mark") != 1 {
			t.Errorf("want exactly one mark (for b), got:\n%s", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an empty highlight word never advances the scan")
	}
}

func TestSweepBasicsCodeBlockLongestWordWins(t *testing.T) {
	out := string(CodeBlock(CodeBlockConfig{Code: "error", HighlightWords: []string{"err", "error"}}))
	if !strings.Contains(out, `<mark class="fui-code-block__mark">error</mark>`) || strings.Count(out, "<mark") != 1 {
		t.Errorf("overlapping words must mark the longest once:\n%s", out)
	}
}

func TestSweepBasicsUnnamedCopyBlocksTargetTheirOwnBody(t *testing.T) {
	target := regexp.MustCompile(`data-hui-copy-target="([^"]+)"`)
	a := string(CodeBlock(CodeBlockConfig{Code: "a", ShowCopy: true}))
	b := string(CodeBlock(CodeBlockConfig{Code: "b", ShowCopy: true}))
	ta, tb := target.FindStringSubmatch(a), target.FindStringSubmatch(b)
	if ta == nil || tb == nil || ta[1] == tb[1] {
		t.Fatalf("two unnamed copy blocks must target distinct bodies:\n%s\n%s", a, b)
	}
	if !strings.Contains(a, `id="`+ta[1]+`"`) {
		t.Errorf("copy target %q names no element:\n%s", ta[1], a)
	}
}

func TestSweepBasicsButtonShortcutNeedsID(t *testing.T) {
	if !sweepBasicsPanics(func() { Button(ButtonConfig{Label: "Save", Shortcut: "Mod+S"}) }) {
		t.Error("a Button Shortcut without ID must panic: the chord names the button by its id")
	}
}

func TestSweepBasicsIconOnlyNeedsARegisteredIcon(t *testing.T) {
	for name, fn := range map[string]func(){
		"Button":     func() { Button(ButtonConfig{Label: "Close", Icon: "no-such-icon", IconOnly: true}) },
		"LinkButton": func() { LinkButton(LinkButtonConfig{Label: "Close", Href: "/", IconOnly: true}) },
	} {
		if !sweepBasicsPanics(fn) {
			t.Errorf("%s IconOnly with no registered icon must panic instead of drawing an empty button", name)
		}
	}
}

func TestSweepBasicsIconOwnsItsAccessibilityAttrs(t *testing.T) {
	out := string(Icon("check", IconConfig{AriaLabel: "Done", ExtraAttrs: map[string]string{"aria-hidden": "true", "role": "presentation"}}))
	if strings.Contains(out, `aria-hidden`) || !strings.Contains(out, `role="img"`) {
		t.Errorf("a labelled icon must keep role=img and drop a caller's aria-hidden:\n%s", out)
	}
}

func TestSweepBasicsButtonSheetLoadsOnEveryPage(t *testing.T) {
	if buttonStyle.Entry().Load != registry.LoadAlways {
		t.Error("the ui-button sheet must be LoadAlways, or a button paints as a native control before its sheet arrives")
	}
}

func TestSweepBasicsAvatarRootDoesNotClip(t *testing.T) {
	root := ruleBody(t, avatarCSS(style.DefaultTheme()), `[data-cui-comp="ui-avatar"]`)
	if strings.Contains(root, "overflow: hidden") || !strings.Contains(root, "position: relative") {
		t.Errorf("the avatar root must be position: relative without overflow: hidden, or the presence dot is clipped:\n%s", root)
	}
}

func TestSweepBasicsPageHeaderClaimsNoRole(t *testing.T) {
	out := string(PageHeader(PageHeaderConfig{Title: "Orders", ExtraAttrs: map[string]string{"role": "banner"}}))
	if strings.Contains(out, "role=") {
		t.Errorf("PageHeader must render a plain <header> and drop a caller's role:\n%s", out)
	}
}
