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
		"ui-terminal-block__head",
		"ui-terminal-block__dot",
		"$ install",
		"ui-terminal-block__body",
		"$ go install ...",
		"→ done",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("TerminalBlock missing %q\n%s", want, h)
		}
	}
}

func TestTerminalLineTones(t *testing.T) {
	if got := string(TerminalOut("x")); !strings.Contains(got, "ui-terminal-block__out") {
		t.Errorf("TerminalOut should carry the muted class:\n%s", got)
	}
	if got := string(TerminalOK("y")); !strings.Contains(got, "ui-terminal-block__ok") {
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
