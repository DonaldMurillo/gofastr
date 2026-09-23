package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTerminalBlockRendersHeadDotBodyAndLines(t *testing.T) {
	h := string(TerminalBlock(TerminalBlockConfig{Label: "$ install"},
		render.Text("$ go install ...\n"),
		TerminalOK("→ done\n"),
	))
	for _, want := range []string{
		`data-fui-comp="ui-terminal-block"`,
		"fui-terminal-block__head",
		"fui-terminal-block__dot",
		"$ install",
		"fui-terminal-block__body",
		"$ go install ...",
		"→ done",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("TerminalBlock missing %q\n%s", want, h)
		}
	}
}

func TestTerminalLineTones(t *testing.T) {
	if got := string(TerminalOut("x")); !classTokenPresent(got, "fui-terminal-block__out") {
		t.Errorf("TerminalOut should carry the muted class:\n%s", got)
	}
	if got := string(TerminalOK("y")); !classTokenPresent(got, "fui-terminal-block__ok") {
		t.Errorf("TerminalOK should carry the success class:\n%s", got)
	}
}

func TestTerminalBlockRequiresLabel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("TerminalBlock with empty Label should panic")
		}
	}()
	TerminalBlock(TerminalBlockConfig{})
}

func TestTerminalBlockExtraAttrsOnRoot(t *testing.T) {
	h := TerminalBlock(TerminalBlockConfig{
		Label:      "$ install",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	}, render.Text("go install"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("wrapper div missing data-test:\n%s", root)
	}
}
