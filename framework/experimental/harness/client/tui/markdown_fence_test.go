package tui

import (
	"strings"
	"testing"
)

// Lines inside a fenced code block are code, shown as written. Styled as
// markdown, a shell comment lost its "#" and became a bold heading, and
// Python's **kwargs lost its stars and went bold up to the next "**".
func TestDrawLeavesFencedCodeAsWritten(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.height = 40
	tui.scrollback = []string{
		"← Run this:",
		"```bash",
		"# install the deps",
		"pip install -r requirements.txt",
		"```",
		"and call `f(**opts)`:",
		"```python",
		"def f(*args, **kwargs): return g(**kwargs)",
		"```",
		"# After",
		"→ next prompt",
		"```",
		"# an unclosed fence in one message does not reach the next",
		"← **done**",
	}
	tui.draw()
	out := stripANSI(buf.String())
	for _, want := range []string{
		"# install the deps",
		"def f(*args, **kwargs): return g(**kwargs)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fenced code line %q was rewritten; drawn:\n%s", want, out)
		}
	}
	raw := buf.String()
	if strings.Contains(raw, ansiBold+"install the deps") {
		t.Errorf("a shell comment in a code block drew as a heading")
	}
	// Outside the fences markdown still applies.
	if !strings.Contains(raw, ansiBold+"After") {
		t.Errorf("the heading after the code block lost its style; drawn:\n%q", raw)
	}
	if strings.Contains(out, "**done**") || !strings.Contains(raw, ansiBold+"done") {
		t.Errorf("an unclosed fence in an earlier message swallowed the next one's bold; drawn:\n%s", out)
	}
}

// The fence that opens a code block may have scrolled off the top: the
// lines still on screen are code all the same.
func TestDrawKnowsAFenceOpenedAboveTheWindow(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.scrollback = []string{"← Script:", "```bash"}
	for range 30 {
		tui.scrollback = append(tui.scrollback, "# step")
	}
	tui.draw()
	if out := stripANSI(buf.String()); strings.Count(out, "# step") == 0 {
		t.Errorf("code under a fence that scrolled off drew as headings:\n%s", out)
	}
}
