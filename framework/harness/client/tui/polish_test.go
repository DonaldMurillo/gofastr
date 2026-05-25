package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestDrawAppliesGutter: every rendered scrollback row should start
// with the 2-space gutter, never flush against column 0.
func TestDrawAppliesGutter(t *testing.T) {
	tui, buf := newRenderTestTUI(t)
	tui.scrollback = []string{"hello"}
	tui.draw()
	out := stripANSI(buf.String())
	// Find the scrollback line (skip the status line which uses
	// reverse-video padding).
	lines := strings.Split(out, "\r\n")
	// Line 1 (after status) should start with at least 2 spaces.
	for i, line := range lines {
		if i == 0 {
			continue // status
		}
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line %d missing gutter: %q", i, line)
		}
		break
	}
}

// TestColorizeMarkerWrapsMarkerOnly verifies the marker glyph is
// wrapped in its color but the content after is left raw so the
// user's terminal theme drives readability.
func TestColorizeMarkerWrapsMarkerOnly(t *testing.T) {
	got := colorizeMarker("← assistant text")
	if !strings.Contains(got, ansiCyan) {
		t.Errorf("missing cyan: %q", got)
	}
	if !strings.Contains(got, "assistant text") {
		t.Errorf("content lost: %q", got)
	}
}

// TestThinkingCollapseAfterText: when a real TextDelta arrives after
// a thinking burst, the burst is replaced with a single
// "* Cogitated for Xs" line.
func TestThinkingCollapseAfterText(t *testing.T) {
	// Freeze time so we can assert on the duration string.
	defer func(old func() int64) { nowFn = old }(nowFn)
	var clock int64
	nowFn = func() int64 { return clock }

	tui := &TUI{Session: ids.NewSessionID(), out: &bytes.Buffer{}}
	clock = 1_000_000_000
	quoted, _ := json.Marshal("Let me think about this hard problem")
	tEnv, _ := control.EncodeEvent(1, control.ThinkingDelta{Block: quoted},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(tEnv)
	// 5 seconds later, the model emits a real reply.
	clock += 5 * 1_000_000_000
	rEnv, _ := control.EncodeEvent(2, control.TextDelta{Text: "Here's my answer"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(rEnv)

	// The thinking line should be gone; a "… cogitated for 5s" line
	// should be present.
	var sawCogitated bool
	var sawRawThinking bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "cogitated for 5s") {
			sawCogitated = true
		}
		if strings.Contains(line, "Let me think about this hard problem") {
			sawRawThinking = true
		}
	}
	if !sawCogitated {
		t.Errorf("collapse summary missing: %v", tui.scrollback)
	}
	if sawRawThinking {
		t.Errorf("raw thinking still visible after collapse: %v", tui.scrollback)
	}
}

// TestCogitatedFor formats durations correctly.
func TestCogitatedFor(t *testing.T) {
	cases := map[int64]string{
		1_000_000_000:               "cogitated for 1s",
		8 * 1_000_000_000:           "cogitated for 8s",
		(60 + 7) * 1_000_000_000:    "cogitated for 1m 7s",
		(8*60 + 7) * 1_000_000_000:  "cogitated for 8m 7s",
	}
	for ns, want := range cases {
		got := cogitatedFor(ns)
		if got != want {
			t.Errorf("cogitatedFor(%d) = %q, want %q", ns, got, want)
		}
	}
}

// TestTurnEndedAddsSeparator: TurnEnded inserts a horizontal-rule
// scrollback entry so consecutive turns are visually separated.
func TestTurnEndedAddsSeparator(t *testing.T) {
	tui, _ := newRenderTestTUI(t)
	tui.width = 80
	env, _ := control.EncodeEvent(1, control.TurnEnded{Turn: 1, Reason: "complete"},
		tui.Session, ids.NewClientID(), time.Now())
	tui.renderEvent(env)
	// Last-two scrollback entries should include the rule.
	if len(tui.scrollback) < 2 {
		t.Fatalf("not enough scrollback entries: %v", tui.scrollback)
	}
	if !strings.HasPrefix(tui.scrollback[len(tui.scrollback)-2], "── ") {
		t.Errorf("missing separator rule: %q", tui.scrollback[len(tui.scrollback)-2])
	}
}

// TestRenderMarkdownInlineBold turns **text** into ANSI-bold.
func TestRenderMarkdownInlineBold(t *testing.T) {
	got := renderMarkdownInline("hello **world**")
	if !strings.Contains(got, ansiBold) {
		t.Errorf("bold not applied: %q", got)
	}
	if !strings.Contains(got, "world") {
		t.Errorf("bold content lost: %q", got)
	}
	// The literal `**` markers should not survive into the styled output.
	if strings.Contains(stripANSI(got), "**") {
		t.Errorf("literal ** survived: %q", stripANSI(got))
	}
}

// TestRenderMarkdownInlineCode turns `code` into ANSI-dim with the
// backticks preserved (so the user can still see it's code).
func TestRenderMarkdownInlineCode(t *testing.T) {
	got := renderMarkdownInline("run `ls -la` first")
	if !strings.Contains(got, ansiDim) {
		t.Errorf("code dim not applied: %q", got)
	}
	if !strings.Contains(stripANSI(got), "`ls -la`") {
		t.Errorf("code content lost: %q", stripANSI(got))
	}
}

// TestRenderMarkdownHeadingsBolded turns `### Heading` into bold +
// strips the # markers.
func TestRenderMarkdownHeadingsBolded(t *testing.T) {
	got := renderMarkdownInline("### File Operations")
	if !strings.Contains(got, ansiBold) {
		t.Errorf("heading not bold: %q", got)
	}
	if strings.Contains(stripANSI(got), "### ") {
		t.Errorf("heading hash markers survived: %q", stripANSI(got))
	}
	if !strings.Contains(stripANSI(got), "File Operations") {
		t.Errorf("heading content lost: %q", stripANSI(got))
	}
}

// TestMarkdownLeavesUnmarkedTextAlone: plain text passes through
// unchanged.
func TestMarkdownLeavesUnmarkedTextAlone(t *testing.T) {
	got := renderMarkdownInline("plain text with no markup")
	if got != "plain text with no markup" {
		t.Errorf("plain text modified: %q", got)
	}
}
