package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func numbered(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s-%04d", prefix, i)
	}
	return strings.Join(lines, "\n")
}

func TestCtrlOExpandsInPlaceAfterTheScrollbackCapTrims(t *testing.T) {
	tui := newSnapshotTUI()
	pushEvent(t, tui, 1, control.ToolCallStarted{CallID: ids.NewCallID(), Tool: "Bash", Args: json.RawMessage(`{}`)})
	pushEvent(t, tui, 2, control.ToolResult{CallID: ids.NewCallID(), Content: []control.ContentBlock{{Type: "text", Text: numbered("file", 10)}}})
	pushEvent(t, tui, 3, control.TextDelta{Text: numbered("line", 996)})
	if len(tui.scrollback) != maxScrollback {
		t.Fatalf("scrollback holds %d lines before Ctrl-O, want the cap %d", len(tui.scrollback), maxScrollback)
	}
	tui.handleKey([]byte{0x0f})
	if len(tui.scrollback) > maxScrollback {
		t.Fatalf("Ctrl-O grew the scrollback to %d lines, past the cap %d", len(tui.scrollback), maxScrollback)
	}
	all := strings.Join(tui.scrollback, "\n")
	for i := range 996 {
		if want := fmt.Sprintf("line-%04d", i); !strings.Contains(all, want) {
			t.Fatalf("Ctrl-O after a cap trim overwrote %q", want)
		}
	}
	if strings.Contains(all, "Ctrl-O to expand") {
		t.Fatal("Ctrl-O after a cap trim left the hint line in place")
	}
}

func TestThinkingCollapsesWholeAfterTheScrollbackCapTrims(t *testing.T) {
	tui := newSnapshotTUI()
	pushEvent(t, tui, 1, control.TextDelta{Text: numbered("keep", 10)})
	pushEvent(t, tui, 2, control.TurnEnded{})
	block, _ := json.Marshal(numbered("think", 995))
	pushEvent(t, tui, 3, control.ThinkingDelta{Block: block})
	if len(tui.scrollback) != maxScrollback {
		t.Fatalf("scrollback holds %d lines before the collapse, want the cap %d", len(tui.scrollback), maxScrollback)
	}
	pushEvent(t, tui, 4, control.TextDelta{Text: "answer"})
	for _, line := range tui.scrollback {
		if strings.Contains(line, "think-") {
			t.Fatalf("thinking line %q survived the collapse after a cap trim", line)
		}
	}
}

func TestProgressTruncationKeepsUTF8Whole(t *testing.T) {
	tui := newSnapshotTUI()
	pushEvent(t, tui, 1, control.ToolCallProgress{CallID: ids.NewCallID(), Partial: strings.Repeat("é", 300)})
	for _, line := range tui.scrollback {
		if !utf8.ValidString(line) {
			t.Fatalf("progress line holds invalid UTF-8: %q", line)
		}
	}
}

func TestAToolCallEndsAnUnclosedFence(t *testing.T) {
	var f fenceTracker
	f.next("← Run:")
	f.next("```bash")
	if f.next("● Bash(ls)") {
		t.Fatal("a tool call line after an unclosed fence was drawn as code")
	}
}

func TestAnErrorResultShowsEightLines(t *testing.T) {
	tui := newSnapshotTUI()
	pushEvent(t, tui, 1, control.ToolResult{CallID: ids.NewCallID(), IsError: true, Content: []control.ContentBlock{{Type: "text", Text: numbered("err", 12)}}})
	all := strings.Join(tui.scrollback, "\n")
	if !strings.Contains(all, "err-0007") || strings.Contains(all, "err-0008") || !strings.Contains(all, "+4 lines") {
		t.Fatalf("an error result did not show exactly its first eight lines:\n%s", all)
	}
}

func TestWrapLineBreaksAtTheLastSpaceThatFits(t *testing.T) {
	got := wrapLine("aaa bbbbb cc", 6)
	if strings.Join(got, "|") != "aaa|bbbbb|cc" {
		t.Fatalf("wrapLine split a word that fits on its own line: %q", got)
	}
}

func TestModalLineHoldsNoNewline(t *testing.T) {
	if got := sanitizeModalText("a\nb"); got != "a b" {
		t.Fatalf("sanitizeModalText(%q) = %q, want a space for the newline", "a\nb", got)
	}
}

func TestSlashCommandsSplitBetweenLocalAndEngine(t *testing.T) {
	tui, rec := newRecordingTUI()
	typeLine(tui, "/help")
	if len(rec.cmds) != 0 || tui.modal == nil {
		t.Fatalf("/help sent %d commands and opened modal=%v; it is answered by the client alone", len(rec.cmds), tui.modal != nil)
	}
	typeLine(tui, "/sessions:list")
	if len(rec.cmds) != 1 {
		t.Fatalf("an unclaimed slash command sent %d commands, want 1", len(rec.cmds))
	}
	if in, ok := rec.cmds[0].(control.SendInput); !ok || !strings.Contains(fmt.Sprint(in.Content), "/sessions:list") {
		t.Fatalf("an unclaimed slash command was sent as %#v, want SendInput carrying it", rec.cmds[0])
	}
}
