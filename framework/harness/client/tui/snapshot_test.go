package tui

// Snapshot harness — drives the TUI through realistic conversation
// flows in-process, captures the rendered frame as plain text (ANSI
// stripped), and writes it to testdata/snapshots/*.txt so we can
// verify the layout without a real terminal in the loop.
//
// Why in-process and not a PTY:
//   - The TUI writes through an io.Writer; raw-mode termios is only
//     entered when t.Run() is invoked. By calling t.renderEvent +
//     t.draw directly we exercise the exact same render path the real
//     binary would use, minus the terminal setup.
//   - No external deps (creack/pty etc.) and no per-OS PTY plumbing.
//   - Snapshots are diff-friendly text files we can read and grep.
//
// To regenerate snapshots after intentional layout changes:
//   GOFASTR_UPDATE_SNAPSHOTS=1 go test ./framework/harness/client/tui/ -run TestSnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	harnessTool "github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
)

const snapshotDir = "testdata/snapshots"

// renderFrame runs draw() and returns the rendered grid as a plain
// text block, by replaying the raw output (including cursor-position
// escapes used by the modal overlay) into a 2D grid of size
// tui.width × tui.height. Each row is right-trimmed of trailing
// spaces; the rows are joined with \r\n. This is a minimal VT
// emulator — enough to capture our own draw output, not a full
// xterm-compatible terminal.
func renderFrame(t *testing.T, tui *TUI) string {
	t.Helper()
	if rb, ok := tui.out.(*bytes.Buffer); ok {
		rb.Reset()
	}
	tui.draw()
	return vtGrid(tui.out.(*bytes.Buffer).String(), tui.width, tui.height)
}

// vtGrid replays raw VT-ish output into a width×height grid. Handles:
//
//   - CSI <r>;<c> H / f  → absolute cursor positioning (1-indexed)
//   - CSI 2 J            → clear screen (we start with a blank grid)
//   - CSI m              → SGR; ignored (snapshot is ANSI-stripped)
//   - CSI <any other>    → consumed and ignored
//   - \r                 → column = 0
//   - \n                 → row += 1
//   - \b                 → column -= 1 (no wrap)
//   - any other rune     → drawn at cursor, cursor advances right
func vtGrid(raw string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	grid := make([][]rune, h)
	for i := range grid {
		grid[i] = make([]rune, w)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}
	runes := []rune(raw)
	row, col := 0, 0
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if c == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && (runes[j] < '@' || runes[j] > '~') {
				j++
			}
			if j >= len(runes) {
				break
			}
			params := string(runes[i+2 : j])
			final := runes[j]
			i = j
			if final == 'H' || final == 'f' {
				r, cc := 1, 1
				if params != "" {
					parts := strings.Split(params, ";")
					if parts[0] != "" {
						_, _ = fmt.Sscanf(parts[0], "%d", &r)
					}
					if len(parts) > 1 && parts[1] != "" {
						_, _ = fmt.Sscanf(parts[1], "%d", &cc)
					}
				}
				row, col = r-1, cc-1
				if row < 0 {
					row = 0
				}
				if col < 0 {
					col = 0
				}
			}
			continue
		}
		switch c {
		case '\r':
			col = 0
		case '\n':
			row++
		case '\b':
			if col > 0 {
				col--
			}
		default:
			if row >= 0 && row < h && col >= 0 && col < w {
				grid[row][col] = c
			}
			col++
			if col >= w {
				col = w - 1
			}
		}
	}
	lines := make([]string, h)
	for i, r := range grid {
		lines[i] = strings.TrimRight(string(r), " ")
	}
	return strings.Join(lines, "\r\n")
}

// writeSnapshot saves the frame to testdata/snapshots/<name>.txt. If
// the file already exists and contents differ, the test fails unless
// GOFASTR_UPDATE_SNAPSHOTS=1 is set.
func writeSnapshot(t *testing.T, name, frame string) {
	t.Helper()
	path := filepath.Join(snapshotDir, name+".txt")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", snapshotDir, err)
	}
	// Add a header so the file is self-describing when read in
	// isolation. Width/height are useful context for the reader.
	header := fmt.Sprintf("# snapshot: %s\n# (lines below are the rendered frame, ANSI stripped)\n#\n", name)
	body := header + frame
	if os.Getenv("GOFASTR_UPDATE_SNAPSHOTS") == "1" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		return
	}
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run — write the file so the next run can compare.
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write initial snapshot: %v", err)
		}
		t.Logf("created initial snapshot: %s", path)
		return
	}
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(existing) != body {
		// Write the new candidate alongside so it's easy to diff.
		_ = os.WriteFile(path+".new", []byte(body), 0o644)
		t.Errorf("snapshot drift: %s — wrote %s.new for inspection. "+
			"If the change is intentional, run with GOFASTR_UPDATE_SNAPSHOTS=1.",
			path, path)
	}
}

// newSnapshotTUI returns a TUI sized to a realistic 120x30 terminal,
// wired to a fresh bytes.Buffer.
func newSnapshotTUI() *TUI {
	return &TUI{
		Session: ids.NewSessionID(),
		Profile: "default",
		Model:   "zai:glm-5.1",
		WebURL:  "http://localhost:8421",
		out:     &bytes.Buffer{},
		width:   120,
		height:  40,
	}
}

// pushEvent encodes and renders one event onto the TUI.
func pushEvent(t *testing.T, tui *TUI, seq uint64, ev control.Event) {
	t.Helper()
	env, err := control.EncodeEvent(seq, ev, tui.Session, ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}
	tui.renderEvent(env)
}

// TestSnapshot_Welcome verifies the welcome banner shown when the
// TUI starts: name, status snippet, keybinds. The banner only fires
// on an empty scrollback, so this also exercises that gate. The
// session is forced to a fixed ULID so the snapshot is deterministic
// across runs.
func TestSnapshot_Welcome(t *testing.T) {
	tui := newSnapshotTUI()
	tui.Session = "sess_01HARNESSWELCOMESNAPSHOT"
	tui.showWelcome()
	frame := renderFrame(t, tui)
	writeSnapshot(t, "welcome", frame)
}

// TestSnapshot_FullSession is the canonical end-to-end visual check.
// We script a realistic conversation: user prompt, thinking burst,
// tool call, multi-line tool result, model reply with markdown,
// turn end, then a second turn that triggers a permission prompt.
//
// The resulting snapshot at testdata/snapshots/full_session.txt is
// what the user would actually see in a terminal.
func TestSnapshot_FullSession(t *testing.T) {
	tui := newSnapshotTUI()

	// --- Turn 1: user asks; model thinks; calls Bash; reports. ---
	tui.input = []rune("explain the changes on this worktree")
	tui.submit()

	pushEvent(t, tui, 1, control.ThinkingDelta{Block: jsonStr(t, "Looking at git status... ")})
	pushEvent(t, tui, 2, control.ThinkingDelta{Block: jsonStr(t, "then I'll diff and read untracked files.")})
	pushEvent(t, tui, 3, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"git status"}`),
	})
	pushEvent(t, tui, 4, control.ToolResult{
		CallID: ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "On branch feature/i18n\nChanges not staged for commit:\n  (use \"git add <file>...\" to update what will be committed)\n\n\tmodified:   framework/harness/client/tui/terminal.go"}},
	})
	pushEvent(t, tui, 5, control.TextDelta{Text: "The worktree adds a new permission UI and fixes tool-result wrapping.\n\n### Highlights\n\n- **Multi-line results** are now split per source line\n- `[permission]` prompts route to `AnswerPermission`\n- Glyphs use text-presentation Unicode (no emoji)"})
	pushEvent(t, tui, 6, control.CostIncremented{
		Provider: "zai", Model: "glm-5.1",
		InputTokens: 412, OutputTokens: 158, USD: 0.0093,
	})
	pushEvent(t, tui, 7, control.TurnEnded{Turn: 1, Reason: "complete"})

	// --- Turn 2: follow-up that hits a permission gate. ---
	tui.input = []rune("now stash any local changes")
	tui.submit()

	pushEvent(t, tui, 7, control.ThinkingDelta{Block: jsonStr(t, "Stash is mutating; let me request permission first.")})
	pushEvent(t, tui, 8, control.PermissionRequested{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"git stash push -m 'snapshot'"}`),
	})

	frame := renderFrame(t, tui)
	writeSnapshot(t, "full_session", frame)
}

// TestSnapshot_MultilineToolResult focuses specifically on the bug
// from the screenshot: a tool result with embedded \n must not
// cascade-indent the terminal.
func TestSnapshot_MultilineToolResult(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("git log")
	tui.submit()
	pushEvent(t, tui, 1, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"git log --all --oneline --graph -10"}`),
	})
	pushEvent(t, tui, 2, control.ToolResult{
		CallID: ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "* 3066b37 docs(roadmap,ui,entities): capture framework DX feedback\n| *   f1e9bd6 On feature/i18n: deep-auto-attempt-2\n|/|\\\n| | * d14a38c untracked files on feature/i18n: …\n| * 7c1b2a0 wip\n* a23f868 Merge pull request #16 from DonaldMurillo/codex/worktree-isolation-mode"}},
	})
	frame := renderFrame(t, tui)
	writeSnapshot(t, "multiline_tool_result", frame)
}

// TestSnapshot_LongToolResultTruncates: a 20-line tool result should
// only show the first 4 lines + a dim "… (+16 lines)" hint, so the
// user can scan multiple tool calls without their screen being eaten
// by one verbose `ls` output.
func TestSnapshot_LongToolResultTruncates(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("list everything")
	tui.submit()
	pushEvent(t, tui, 1, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"ls -1 /usr/lib"}`),
	})
	// Build a 20-line result.
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("file-%02d.so", i))
	}
	pushEvent(t, tui, 2, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: strings.Join(lines, "\n")}},
	})
	// Follow with a second tool call so the truncation hint is
	// visually placed between the two calls.
	pushEvent(t, tui, 3, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"pwd"}`),
	})
	pushEvent(t, tui, 4, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: "/Users/dom/programming/gofastr"}},
	})
	frame := renderFrame(t, tui)
	writeSnapshot(t, "long_tool_result_truncates", frame)

	// Direct assertion: scrollback must contain the truncation hint.
	var sawHint bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "(+16 line") {
			sawHint = true
			break
		}
	}
	if !sawHint {
		t.Errorf("missing truncation hint in scrollback: %v", tui.scrollback)
	}
}

// TestSnapshot_SlashPopupOnSolitarySlash: typing just `/` should show
// the live candidate popup above the input box (no Tab required).
func TestSnapshot_SlashPopupOnSolitarySlash(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/")
	frame := renderFrame(t, tui)
	writeSnapshot(t, "slash_popup_all", frame)
}

// TestSnapshot_SlashPopupNarrows: typing `/c` should filter the popup
// to just /clear, /compact, /cost.
func TestSnapshot_SlashPopupNarrows(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	frame := renderFrame(t, tui)
	writeSnapshot(t, "slash_popup_narrowed", frame)
}

// TestSlashPopupItemsAll: a solitary `/` matches every builtin +
// namespace. With maxRows=8 and 15 total candidates, expect the
// "(+N more)" overflow row.
func TestSlashPopupItemsAll(t *testing.T) {
	rows, _, total := slashPopupItems("/", 8)
	if total < 15 {
		t.Errorf("expected ≥15 total candidates, got %d", total)
	}
	if len(rows) != 8 {
		t.Errorf("expected 8 visible rows (cap), got %d", len(rows))
	}
	if !strings.Contains(rows[len(rows)-1], "more") {
		t.Errorf("last row should be overflow hint: %q", rows[len(rows)-1])
	}
}

// TestSlashPopupItemsFiltered: /c narrows the list.
func TestSlashPopupItemsFiltered(t *testing.T) {
	rows, _, total := slashPopupItems("/c", 8)
	if total != 3 {
		t.Errorf("expected 3 candidates (clear/compact/cost), got %d: %v", total, rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, "/c") {
			t.Errorf("non-matching row leaked: %q", row)
		}
	}
}

// TestSlashPopupItemsNonSlash: text input returns no popup.
func TestSlashPopupItemsNonSlash(t *testing.T) {
	rows, _, total := slashPopupItems("hello", 8)
	if rows != nil || total != 0 {
		t.Errorf("non-slash input returned popup: rows=%v total=%d", rows, total)
	}
}

// TestTabCompletesToFirstChoice: with multiple matches and no
// arrow-key navigation, Tab inserts the first candidate (not the
// longest common prefix).
func TestTabCompletesToFirstChoice(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	tui.handleTab()
	// /c matches /clear, /compact, /cost (sorted: clear first).
	want := "/clear "
	if string(tui.input) != want {
		t.Errorf("Tab on /c => %q, want %q", string(tui.input), want)
	}
}

// TestArrowNavigatesPopup: when input starts with /, Up/Down move
// the popup selection rather than scrolling.
func TestArrowNavigatesPopup(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	if !tui.popupActive() {
		t.Fatal("popup not active for /c")
	}
	// Down once → idx 1 (=/compact).
	tui.handleEscape([]byte("\x1b[B"))
	if tui.popupIdx != 1 {
		t.Errorf("after Down, idx = %d, want 1", tui.popupIdx)
	}
	// Tab should now complete to /compact.
	tui.handleTab()
	if string(tui.input) != "/compact " {
		t.Errorf("Tab after Down(1) => %q, want /compact", string(tui.input))
	}
}

// TestArrowWrapsAtEnds: Up at idx 0 wraps to the last candidate;
// Down at the last wraps back to 0.
func TestArrowWrapsAtEnds(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c") // 3 candidates
	tui.handleEscape([]byte("\x1b[A"))
	if tui.popupIdx != 2 {
		t.Errorf("Up from 0 should wrap to last (2), got %d", tui.popupIdx)
	}
	tui.handleEscape([]byte("\x1b[B"))
	if tui.popupIdx != 0 {
		t.Errorf("Down from last should wrap to 0, got %d", tui.popupIdx)
	}
}

// TestTypingResetsPopupIdx: any character or backspace must zero
// the selection so the popup doesn't keep a stale highlight after
// the candidate list changes.
func TestTypingResetsPopupIdx(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	tui.popupIdx = 2
	tui.handleKey([]byte{'l'})
	if tui.popupIdx != 0 {
		t.Errorf("typing did not reset popupIdx: %d", tui.popupIdx)
	}
	tui.popupIdx = 1
	tui.handleKey([]byte{0x7f}) // backspace
	if tui.popupIdx != 0 {
		t.Errorf("backspace did not reset popupIdx: %d", tui.popupIdx)
	}
}

// TestEnterCompletesWhenPopupAmbiguous: with multiple matches in the
// popup, Enter completes the highlighted candidate instead of
// submitting. After the completion the input must reflect the new
// value and no SendInput should have been sent.
func TestEnterCompletesWhenPopupAmbiguous(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c") // 3 matches: clear / compact / cost
	exit := tui.handleKey([]byte{0x0d})
	if exit {
		t.Fatal("Enter on ambiguous popup should not exit")
	}
	if string(tui.input) != "/clear " {
		t.Errorf("Enter on /c => %q, want /clear ", string(tui.input))
	}
	// Scrollback should NOT contain a "→ /c" submission — Enter was
	// intercepted by the popup.
	for _, line := range tui.scrollback {
		if strings.Contains(line, "→ /c") {
			t.Errorf("Enter accidentally submitted /c: %q", line)
		}
	}
}

// TestEnterAfterCompletionSubmits: once the popup is dismissed (by
// a prior completion landing on `/help `), the next Enter must
// submit normally — the autocomplete hijack must not be sticky.
// We use /help because it opens a modal on dispatch (rather than
// wiping scrollback like /clear), so the submission line stays
// visible for inspection.
func TestEnterAfterCompletionSubmits(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/hel") // unique prefix → completes to /help
	// First Enter — completes.
	tui.handleKey([]byte{0x0d})
	if string(tui.input) != "/help " {
		t.Fatalf("first Enter didn't complete: %q", string(tui.input))
	}
	// Second Enter — should submit. /help opens a modal, so we check
	// that the modal is now active AND the submission line landed in
	// scrollback.
	tui.handleKey([]byte{0x0d})
	if string(tui.input) != "" {
		t.Errorf("second Enter didn't clear input: %q", string(tui.input))
	}
	if tui.modal == nil {
		t.Error("second Enter did not dispatch /help (modal not open)")
	}
	var sawSubmit bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "→ /help") {
			sawSubmit = true
			break
		}
	}
	if !sawSubmit {
		t.Errorf("second Enter did not echo submission to scrollback: %v", tui.scrollback)
	}
}

// TestEnterSubmitsWhenInputIsExactMatch: when the input already
// equals a candidate exactly (e.g. user types `/clear` letter by
// letter), Enter must submit without round-tripping through a no-op
// completion. Otherwise the user would have to press Enter twice
// just because the popup happened to be open.
func TestEnterSubmitsWhenInputIsExactMatch(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/clear")
	if tui.popupAcceptsEnter() {
		t.Error("popupAcceptsEnter should be false when input matches exactly")
	}
	tui.handleKey([]byte{0x0d})
	if string(tui.input) != "" {
		t.Errorf("Enter on exact /clear did not submit (input still %q)", string(tui.input))
	}
}

// TestEnterAfterDownArrow: navigating with Down then pressing Enter
// completes to the highlighted candidate.
func TestEnterAfterDownArrow(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	tui.handleEscape([]byte("\x1b[B")) // Down
	tui.handleKey([]byte{0x0d})        // Enter
	if string(tui.input) != "/compact " {
		t.Errorf("Down + Enter => %q, want /compact ", string(tui.input))
	}
}

// TestTurnStartedFromOtherClientRenders: a TurnStarted event from a
// DIFFERENT originator (e.g. the browser sidecar) must show up as
// `→ msg` in the TUI scrollback, so multi-client sessions don't
// look like the model replied to nothing.
func TestTurnStartedFromOtherClientRenders(t *testing.T) {
	tui := newSnapshotTUI()
	// Force a known client id on the TUI (no inproc.Client attached).
	otherClient := ids.NewClientID()
	env, _ := control.EncodeEvent(1, control.TurnStarted{
		Turn:       1,
		Originator: otherClient,
		Content: []control.ContentBlock{
			{Type: "text", Text: "hello from the browser"},
		},
	}, tui.Session, otherClient, time.Now())
	tui.renderEvent(env)
	var seen bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "→ hello from the browser") {
			seen = true
			break
		}
	}
	if !seen {
		t.Errorf("non-self TurnStarted not rendered as → user message: %v", tui.scrollback)
	}
}

// TestTurnStartedFromSelfIsSkipped: when TurnStarted's originator
// matches the TUI's ClientID, we must NOT render — submit() already
// echoed the line locally and we'd double-print otherwise.
func TestTurnStartedFromSelfIsSkipped(t *testing.T) {
	tui := newSnapshotTUI()
	tui.ClientID = ids.NewClientID()
	env, _ := control.EncodeEvent(1, control.TurnStarted{
		Turn:       1,
		Originator: tui.ClientID,
		Content: []control.ContentBlock{
			{Type: "text", Text: "my own message"},
		},
	}, tui.Session, tui.ClientID, time.Now())
	tui.renderEvent(env)
	for _, line := range tui.scrollback {
		if strings.Contains(line, "my own message") {
			t.Errorf("self-originated TurnStarted leaked into scrollback: %q", line)
		}
	}
}

// TestSlashWebShowsUrlAndToken: when WebURL + WebToken are populated,
// /web opens a modal listing both plus a curl snippet.
func TestSlashWebShowsUrlAndToken(t *testing.T) {
	tui := newSnapshotTUI()
	tui.WebURL = "http://127.0.0.1:18424"
	tui.WebToken = "eyJ2ZXIiOjEsImp0aSI6IkFCQyJ9.signature-bytes"
	tui.dispatchLocalSlash("/web")
	if tui.modal == nil {
		t.Fatal("/web did not open a modal")
	}
	body := strings.Join(tui.modal.lines, "\n")
	for _, want := range []string{
		"http://127.0.0.1:18424",
		"eyJ2ZXIiOjEsImp0aSI6IkFCQyJ9.signature-bytes",
		"curl",
		"Bearer",
		"/v1/sessions",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/web modal missing %q\n--- body ---\n%s", want, body)
		}
	}
}

// TestSlashWebWithoutUrlShowsHelp: when no URL is configured the
// modal explains how to enable the sidecar instead of a cryptic
// "(no URL)".
func TestSlashWebWithoutUrlShowsHelp(t *testing.T) {
	tui := newSnapshotTUI()
	tui.WebURL = ""
	tui.dispatchLocalSlash("/web")
	if tui.modal == nil {
		t.Fatal("/web did not open a modal")
	}
	body := strings.Join(tui.modal.lines, "\n")
	if !strings.Contains(body, "-web") {
		t.Errorf("/web no-URL modal missing -web hint:\n%s", body)
	}
}

// TestEnterOnNonSlashSubmitsImmediately: regular text input still
// submits on first Enter — the popup hijack is slash-only.
func TestEnterOnNonSlashSubmitsImmediately(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("hello world")
	tui.handleKey([]byte{0x0d})
	if string(tui.input) != "" {
		t.Errorf("Enter on regular text did not submit (input still %q)", string(tui.input))
	}
	var sawSubmit bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "→ hello world") {
			sawSubmit = true
			break
		}
	}
	if !sawSubmit {
		t.Errorf("scrollback missing user submission: %v", tui.scrollback)
	}
}

// TestArrowStillScrollsWhenPopupInactive: Up/Down on non-slash input
// goes back to the existing scroll behavior.
func TestArrowStillScrollsWhenPopupInactive(t *testing.T) {
	tui := newSnapshotTUI()
	// fill scrollback so we have something to scroll past
	for i := 0; i < 50; i++ {
		tui.scrollback = append(tui.scrollback, "filler")
	}
	tui.input = []rune("hello world") // not a slash command
	before := tui.scrollOffset
	tui.handleEscape([]byte("\x1b[A"))
	if tui.scrollOffset == before {
		t.Errorf("Up arrow on non-slash input did not scroll: offset still %d", before)
	}
}

// TestSnapshot_CtrlOExpandsTruncated: after a truncated tool result,
// Ctrl-O splices the elided lines back into scrollback.
func TestSnapshot_CtrlOExpandsTruncated(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("list")
	tui.submit()
	pushEvent(t, tui, 1, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"ls /usr/lib"}`),
	})
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("file-%02d.so", i))
	}
	pushEvent(t, tui, 2, control.ToolResult{
		CallID:  ids.NewCallID(),
		Content: []control.ContentBlock{{Type: "text", Text: strings.Join(lines, "\n")}},
	})
	// Press Ctrl-O.
	tui.handleKey([]byte{0x0f})
	frame := renderFrame(t, tui)
	writeSnapshot(t, "ctrl_o_expand", frame)

	// All 10 files must be visible.
	for i := 1; i <= 10; i++ {
		want := fmt.Sprintf("file-%02d.so", i)
		var ok bool
		for _, line := range tui.scrollback {
			if strings.Contains(line, want) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("after Ctrl-O, missing %q in scrollback: %v", want, tui.scrollback)
		}
	}
	// And the truncated-span tracker must be empty.
	if len(tui.truncated) != 0 {
		t.Errorf("truncated tracker not emptied after expand: %d entries", len(tui.truncated))
	}
}

// TestSnapshot_HelpModal: /help opens a centered modal instead of
// dumping to scrollback.
func TestSnapshot_HelpModal(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/help")
	tui.submit()
	frame := renderFrame(t, tui)
	writeSnapshot(t, "help_modal", frame)
	if tui.modal == nil {
		t.Error("/help did not open modal")
	}
	if tui.modal.title != "Help" {
		t.Errorf("modal title = %q, want Help", tui.modal.title)
	}
}

// TestModalDismissedByEscape: Esc closes an active modal.
func TestModalDismissedByEscape(t *testing.T) {
	tui := newSnapshotTUI()
	tui.openModal("Test", []string{"hi"})
	if tui.modal == nil {
		t.Fatal("modal not opened")
	}
	tui.modalKey([]byte{0x1b})
	if tui.modal != nil {
		t.Error("Esc did not dismiss modal")
	}
}

// TestModalIgnoresOtherKeys: while a modal is open, handleKey routes
// keys to modalKey instead of the input buffer.
func TestModalRoutesKeysAway(t *testing.T) {
	tui := newSnapshotTUI()
	tui.openModal("Test", []string{"hi"})
	tui.handleKey([]byte{'a'})
	if string(tui.input) != "" {
		t.Errorf("modal did not absorb key: input = %q", string(tui.input))
	}
	if tui.modal == nil {
		t.Error("modal closed unexpectedly")
	}
	// Enter dismisses.
	tui.handleKey([]byte{0x0d})
	if tui.modal != nil {
		t.Error("Enter did not close modal")
	}
}

// TestScrollbackSearch_FindsCaseInsensitive: searchScrollback returns
// the indices of all matches, case-insensitive, in scrollback order.
func TestScrollbackSearch_FindsCaseInsensitive(t *testing.T) {
	lines := []string{
		"first line about Bash",
		"middle line about nothing",
		"third line about bashing the keyboard",
		"BASH something else",
	}
	hits := searchScrollback(lines, "bash")
	if len(hits) != 3 {
		t.Errorf("got %d hits, want 3: %v", len(hits), hits)
	}
	// hits should be {0, 2, 3}
	wantSet := map[int]bool{0: true, 2: true, 3: true}
	for _, h := range hits {
		if !wantSet[h] {
			t.Errorf("unexpected hit index %d", h)
		}
	}
}

// TestScrollbackSearch_EmptyQueryReturnsNothing: prevents the
// "search for empty string" pathological case (would match every line).
func TestScrollbackSearch_EmptyQueryReturnsNothing(t *testing.T) {
	hits := searchScrollback([]string{"a", "b"}, "")
	if len(hits) != 0 {
		t.Errorf("empty query should return no hits: %v", hits)
	}
}

// TestSearchModeActivation: Ctrl-R toggles search mode; typing
// while in search mode filters; Esc cancels and restores.
func TestSearchModeActivation(t *testing.T) {
	tui := newSnapshotTUI()
	tui.scrollback = []string{"hello world", "foo bar", "say hello again"}
	tui.handleKey([]byte{0x12}) // Ctrl-R
	if !tui.searchActive {
		t.Fatal("Ctrl-R didn't activate search mode")
	}
	for _, c := range []byte("hello") {
		tui.handleKey([]byte{c})
	}
	if tui.searchQuery != "hello" {
		t.Errorf("search query = %q, want hello", tui.searchQuery)
	}
	if len(tui.searchHits) != 2 {
		t.Errorf("got %d hits, want 2 for 'hello'", len(tui.searchHits))
	}
	tui.handleKey([]byte{0x1b}) // Esc
	if tui.searchActive {
		t.Error("Esc didn't cancel search mode")
	}
	if tui.searchQuery != "" {
		t.Error("Esc didn't clear search query")
	}
}

// TestMultilineInput_CtrlJInsertsNewline: Ctrl-J (LF) should
// insert a newline into the input buffer instead of submitting.
// Enter (CR / 0x0d) still submits.
func TestMultilineInput_CtrlJInsertsNewline(t *testing.T) {
	tui := newSnapshotTUI()
	for _, c := range []byte{'a', 'b'} {
		tui.handleKey([]byte{c})
	}
	if exit := tui.handleKey([]byte{0x0a}); exit {
		t.Fatal("Ctrl-J should not exit")
	}
	tui.handleKey([]byte{'c'})
	if string(tui.input) != "ab\nc" {
		t.Errorf("input = %q, want \"ab\\nc\"", string(tui.input))
	}
}

// TestMultilineInput_EnterStillSubmits: regular CR (0x0d) Enter
// still submits — only LF (Ctrl-J) is the newline insert.
func TestMultilineInput_EnterStillSubmits(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("hello")
	tui.handleKey([]byte{0x0d})
	if string(tui.input) != "" {
		t.Errorf("Enter didn't submit: input = %q", string(tui.input))
	}
}

// TestSpinnerFrameCycles: the spinner advances through a deterministic
// set of frames so the TUI can render an animated "waiting" indicator
// without depending on wall time.
func TestSpinnerFrameCycles(t *testing.T) {
	frames := []rune{}
	for i := 0; i < 12; i++ {
		frames = append(frames, spinnerGlyph(i))
	}
	// At least 8 distinct frames (Braille set has 10).
	seen := map[rune]bool{}
	for _, r := range frames {
		seen[r] = true
	}
	if len(seen) < 8 {
		t.Errorf("spinner only has %d unique frames, want ≥ 8: %v", len(seen), frames)
	}
}

// TestTurnStartedShowsSpinnerRow: on TurnStarted (from self), a
// spinner row should appear so the user sees the agent is working
// even before the first thinking/text delta arrives. The row uses
// the current frame from spinnerGlyph.
func TestTurnStartedShowsSpinnerRow(t *testing.T) {
	tui := newSnapshotTUI()
	// Other-originator path so we don't suppress (own-input has a
	// separate flow). For self-originated turns, the spinner shows
	// for the parent's own turn — let's test that one.
	tui.ClientID = ids.NewClientID()
	tui.input = []rune("hello")
	tui.submit()
	// Now simulate a TurnStarted from "self" — submit() already
	// echoed the user line; the engine would publish TurnStarted
	// next. The TUI should show a spinner row.
	env, _ := control.EncodeEvent(1, control.TurnStarted{
		Turn:       1,
		Originator: tui.ClientID,
		Content:    []control.ContentBlock{{Type: "text", Text: "hello"}},
	}, tui.Session, tui.ClientID, time.Now())
	tui.renderEvent(env)
	// Spinner glyph is substituted at draw() time so the frame can
	// advance without rewriting scrollback. Render the frame and
	// scan it for a Braille pattern char.
	frame := renderFrame(t, tui)
	var sawSpinner bool
	for _, r := range frame {
		if r >= 0x2800 && r <= 0x28FF {
			sawSpinner = true
			break
		}
	}
	if !sawSpinner {
		t.Errorf("no Braille spinner glyph in rendered frame:\n%s", frame)
	}
}

// TestSlash_Profile_OpensModal: /profile shows the current profile +
// model in a modal so the user can see what's loaded without
// querying the server.
func TestSlash_Profile_OpensModal(t *testing.T) {
	tui := newSnapshotTUI()
	tui.Profile = "default"
	tui.Model = "zai:glm-5.1"
	handled, _ := tui.dispatchLocalSlash("/profile")
	if !handled {
		t.Fatal("/profile not handled")
	}
	if tui.modal == nil {
		t.Fatal("/profile didn't open modal")
	}
	body := strings.Join(tui.modal.lines, "\n")
	if !strings.Contains(body, "default") || !strings.Contains(body, "zai:glm-5.1") {
		t.Errorf("/profile modal missing profile/model: %s", body)
	}
}

// TestSlash_Cost_OpensModal: /cost shows the accumulated USD + token
// breakdown from the in-process counter.
func TestSlash_Cost_OpensModal(t *testing.T) {
	tui := newSnapshotTUI()
	tui.costUSD = 0.0123
	handled, _ := tui.dispatchLocalSlash("/cost")
	if !handled {
		t.Fatal("/cost not handled")
	}
	if tui.modal == nil {
		t.Fatal("/cost didn't open modal")
	}
	body := strings.Join(tui.modal.lines, "\n")
	if !strings.Contains(body, "$0.0123") {
		t.Errorf("/cost modal missing the USD: %s", body)
	}
}

// TestSlash_Health_OpensModal: /health shows subsystem status — for
// the TUI we mostly care that it dispatches and renders something
// (the real REST handler does the deep check; this is the UI hook).
func TestSlash_Health_OpensModal(t *testing.T) {
	tui := newSnapshotTUI()
	tui.WebURL = "http://127.0.0.1:18421"
	handled, _ := tui.dispatchLocalSlash("/health")
	if !handled {
		t.Fatal("/health not handled")
	}
	if tui.modal == nil {
		t.Fatal("/health didn't open modal")
	}
}

// TestSlash_Model_NoArg_ShowsCurrent: /model with no arg shows what's
// currently active. With an arg it queues a SetModel command.
func TestSlash_Model_NoArg_ShowsCurrent(t *testing.T) {
	tui := newSnapshotTUI()
	tui.Model = "zai:glm-5.1"
	tui.dispatchLocalSlash("/model")
	if tui.modal == nil {
		t.Fatal("/model with no arg should open a modal showing current")
	}
	body := strings.Join(tui.modal.lines, "\n")
	if !strings.Contains(body, "zai:glm-5.1") {
		t.Errorf("/model modal missing current model: %s", body)
	}
}

// TestSlash_Compact_EmitsScrollbackHint: /compact is best-effort
// client side — it adds a scrollback hint and (when wired) issues
// a CustomCommand. We just verify it's handled and the user gets
// feedback.
func TestSlash_Compact_DispatchesLocally(t *testing.T) {
	tui := newSnapshotTUI()
	handled, _ := tui.dispatchLocalSlash("/compact")
	if !handled {
		t.Fatal("/compact not handled")
	}
	// Either the scrollback gets a hint or a modal opens — both fine.
	if tui.modal == nil && len(tui.scrollback) == 0 {
		t.Errorf("/compact produced no visible feedback")
	}
}

// TestSlashTasksShowsPlanInModal: /tasks opens a modal that lists
// every task currently stored for this session, with an icon per
// status. Exercises the wiring between TUI ↔ builtins.TaskListSnapshot
// without going through a real LLM.
func TestSlashTasksShowsPlanInModal(t *testing.T) {
	tui := newSnapshotTUI()
	// Seed a plan directly via the tool — same path the LLM would use.
	tl := builtins.TaskList{}
	raw, _ := json.Marshal(map[string]any{"tasks": []builtins.TaskItem{
		{Content: "audit imports", Status: "completed"},
		{Content: "fix race", Status: "in_progress", ActiveForm: "Fixing race"},
		{Content: "ship it", Status: "pending"},
	}})
	if _, err := tl.Run(
		harnessTool.WithSession(context.Background(), tui.Session),
		harnessTool.ToolCall{Name: "TaskList", Input: raw}, nil); err != nil {
		t.Fatal(err)
	}
	defer builtins.ResetTasks(tui.Session)
	// Dispatch /tasks.
	handled, _ := tui.dispatchLocalSlash("/tasks")
	if !handled {
		t.Fatal("/tasks not handled locally")
	}
	if tui.modal == nil {
		t.Fatal("/tasks did not open modal")
	}
	body := strings.Join(tui.modal.lines, "\n")
	for _, want := range []string{
		"✓", "audit imports", // completed
		"▸", "Fixing race", // in_progress with activeForm
		"○", "ship it", // pending
		"Last updated",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/tasks modal missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestSlashTasksEmpty(t *testing.T) {
	tui := newSnapshotTUI()
	// Ensure no tasks for this fresh session.
	builtins.ResetTasks(tui.Session)
	tui.dispatchLocalSlash("/tasks")
	if tui.modal == nil {
		t.Fatal("/tasks modal not opened")
	}
	body := strings.Join(tui.modal.lines, "\n")
	if !strings.Contains(body, "No plan yet") {
		t.Errorf("empty-plan modal missing 'No plan yet': %s", body)
	}
}

// TestSnapshot_PermissionPrompt verifies the permission UI lands
// correctly: the inline help line, the `permit?` input prompt, and
// the args displayed alongside the tool name.
func TestSnapshot_PermissionPrompt(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("delete the build dir")
	tui.submit()
	pushEvent(t, tui, 1, control.PermissionRequested{
		CallID: ids.NewCallID(), Tool: "Bash",
		Args: json.RawMessage(`{"cmd":"rm -rf dist/"}`),
	})
	frame := renderFrame(t, tui)
	writeSnapshot(t, "permission_prompt", frame)
}

// TestSnapshot_ThinkingCollapse exercises the "[thinking] → cogitated
// for Xs" transition: a long reasoning burst followed by the model's
// real reply should leave just the summary line, not the full
// stream-of-consciousness.
func TestSnapshot_ThinkingCollapse(t *testing.T) {
	tui := newSnapshotTUI()
	// Freeze time so the elapsed value is deterministic.
	defer func(old func() int64) { nowFn = old }(nowFn)
	var clock int64 = 1_000_000_000
	nowFn = func() int64 { return clock }

	tui.input = []rune("what is 2+2")
	tui.submit()
	clock = 1_000_000_000
	pushEvent(t, tui, 1, control.ThinkingDelta{Block: jsonStr(t, "Let me work through this carefully. ")})
	clock += 3 * 1_000_000_000
	pushEvent(t, tui, 2, control.ThinkingDelta{Block: jsonStr(t, "Two plus two equals four. ")})
	pushEvent(t, tui, 3, control.ThinkingDelta{Block: jsonStr(t, "I am confident in that answer.")})
	clock += 5 * 1_000_000_000
	pushEvent(t, tui, 4, control.TextDelta{Text: "4."})
	pushEvent(t, tui, 5, control.TurnEnded{Turn: 1, Reason: "complete"})

	frame := renderFrame(t, tui)
	writeSnapshot(t, "thinking_collapse", frame)
}

// TestSnapshot_SlashHelp exercises the local `/help` command: typed,
// submitted, and dispatched without the engine. The built-in catalog
// and the namespace list should both land in scrollback.
func TestSnapshot_SlashHelp(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/help")
	tui.submit()
	frame := renderFrame(t, tui)
	writeSnapshot(t, "slash_help", frame)
}

// TestSnapshot_TabCompletionPartial: typing `/he` + Tab should
// auto-complete to `/help`. The input area in the next draw should
// show the completed verb, and no candidate list (since there's only
// one match).
func TestSnapshot_TabCompletionPartial(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/he")
	tui.handleTab()
	frame := renderFrame(t, tui)
	writeSnapshot(t, "tab_completion_partial", frame)
}

// TestSnapshot_TabCompletionAmbiguous: typing `/c` matches both
// `/clear` and `/compact` and `/cost`. The completion lands on the
// longest common prefix `/c`, and the candidate list is printed to
// scrollback.
func TestSnapshot_TabCompletionAmbiguous(t *testing.T) {
	tui := newSnapshotTUI()
	tui.input = []rune("/c")
	tui.handleTab()
	frame := renderFrame(t, tui)
	writeSnapshot(t, "tab_completion_ambiguous", frame)
}

// TestSlashCompletionLogic covers the pure completeSlash function so
// edge cases (no prefix, no matches, single match, ambiguous) are
// locked down without going through draw().
func TestSlashCompletionLogic(t *testing.T) {
	cases := []struct {
		in              string
		wantCompletion  string
		wantMatchesLen  int
	}{
		{"/hel", "/help", 0},   // unique
		{"/hea", "/health", 0}, // unique
		{"/he", "/he", 2},      // ambiguous: help vs health
		{"/clear", "/clear", 0},
		{"/zzz", "", 0},
		{"hi", "", 0}, // not a slash command
	}
	for _, c := range cases {
		comp, matches := completeSlash(c.in)
		if comp != c.wantCompletion {
			t.Errorf("completeSlash(%q) completion = %q, want %q", c.in, comp, c.wantCompletion)
		}
		if c.wantMatchesLen > 0 && len(matches) < 2 {
			t.Errorf("completeSlash(%q) expected multiple matches, got %d", c.in, len(matches))
		}
		if c.wantMatchesLen == 0 && len(matches) > 0 {
			t.Errorf("completeSlash(%q) expected no candidate list, got %v", c.in, matches)
		}
	}
}

// TestDispatchLocalSlashClear: /clear empties scrollback and re-shows
// the welcome banner (so keybind hints remain after a clear).
func TestDispatchLocalSlashClear(t *testing.T) {
	tui := newSnapshotTUI()
	tui.scrollback = []string{"old line 1", "old line 2"}
	handled, exit := tui.dispatchLocalSlash("/clear")
	if !handled {
		t.Error("/clear not handled locally")
	}
	if exit {
		t.Error("/clear should not exit")
	}
	// After /clear the old lines must be gone.
	for _, line := range tui.scrollback {
		if strings.Contains(line, "old line") {
			t.Errorf("/clear did not remove old content: %q", line)
		}
	}
	// And the welcome banner header should be present.
	var sawHeader bool
	for _, line := range tui.scrollback {
		if strings.Contains(line, "gofastr harness") {
			sawHeader = true
			break
		}
	}
	if !sawHeader {
		t.Errorf("/clear did not re-render welcome banner: %v", tui.scrollback)
	}
}

// TestDispatchLocalSlashQuit: /quit signals exit.
func TestDispatchLocalSlashQuit(t *testing.T) {
	tui := newSnapshotTUI()
	handled, exit := tui.dispatchLocalSlash("/quit")
	if !handled || !exit {
		t.Errorf("/quit handled=%v exit=%v, want both true", handled, exit)
	}
}

// TestInputHistoryRecall: Ctrl-P walks back through submitted inputs,
// Ctrl-N walks forward. Past-newest returns to a fresh prompt.
func TestInputHistoryRecall(t *testing.T) {
	tui := newSnapshotTUI()
	for _, msg := range []string{"first", "second", "third"} {
		tui.input = []rune(msg)
		tui.submit()
	}
	// Ctrl-P three times → "third", "second", "first"
	want := []string{"third", "second", "first"}
	for i, w := range want {
		tui.recallHistory(-1)
		if string(tui.input) != w {
			t.Errorf("after %d Ctrl-P, input = %q, want %q", i+1, string(tui.input), w)
		}
	}
	// One more Ctrl-P should stay at "first" (clamped to oldest).
	tui.recallHistory(-1)
	if string(tui.input) != "first" {
		t.Errorf("Ctrl-P past oldest changed input: %q", string(tui.input))
	}
	// Ctrl-N twice → "second", "third"
	tui.recallHistory(+1)
	if string(tui.input) != "second" {
		t.Errorf("after Ctrl-N: %q, want second", string(tui.input))
	}
	tui.recallHistory(+1)
	if string(tui.input) != "third" {
		t.Errorf("after Ctrl-N: %q, want third", string(tui.input))
	}
	// Ctrl-N past newest → blank.
	tui.recallHistory(+1)
	if string(tui.input) != "" {
		t.Errorf("Ctrl-N past newest did not clear input: %q", string(tui.input))
	}
}

// TestCostIncrementedUpdatesStatusLine: a CostIncremented event
// accumulates into t.costUSD and shows in the status line.
func TestCostIncrementedUpdatesStatusLine(t *testing.T) {
	tui := newSnapshotTUI()
	pushEvent(t, tui, 1, control.CostIncremented{
		Provider: "zai", Model: "glm-5.1",
		InputTokens: 100, OutputTokens: 50, USD: 0.0123,
	})
	pushEvent(t, tui, 2, control.CostIncremented{
		Provider: "zai", Model: "glm-5.1",
		InputTokens: 50, OutputTokens: 25, USD: 0.0050,
	})
	status := tui.statusLine()
	// Should show the cumulative $0.0173.
	if !strings.Contains(status, "$0.0173") {
		t.Errorf("status missing cost: %q", status)
	}
}

// jsonStr is shorthand: returns json.Marshal(s) as a json.RawMessage.
// ThinkingDelta.Block is a json.RawMessage that the renderer
// JSON-unquotes, so callers must pass valid JSON-string bytes.
func jsonStr(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
