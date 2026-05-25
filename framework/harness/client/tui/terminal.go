// Package tui is the pure-stdlib + golang.org/x/term TUI client.
//
// v0.1 implemented:
//
//   - Raw-mode termios management (entry/exit, signal handling)
//   - ANSI-based screen rendering (no termcap; VT100/xterm)
//   - Scrollback view with full redraw (diff-based is roadmap)
//   - Single-line input + history recall via Ctrl-P / Ctrl-N
//   - Status line (profile, model, cost meter, web URL)
//   - Inline permission prompt (allow once / session / tool / deny)
//   - Slash-command tab completion + local dispatch
//     (/clear, /help, /web, /quit; others forward to engine)
//   - SGR mouse-wheel scroll + arrow/page/home/end keys
//   - SIGWINCH resize handling
//   - Two-press Ctrl-C exit (with mid-turn cancel)
//
// Out of v0.1 (roadmap):
//
//   - Diff-based redraw (full redraw is fine for typical scrollback)
//   - Multiline input editor (Shift-Enter for newline)
//   - Modal overlay with diff preview for permission prompts
//   - Bracketed paste / kitty keyboard protocol
//   - Syntax highlighting in tool output
//   - Windows ConPTY (Unix-only; Windows ships the web client)
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TUI is the bundled terminal client. It attaches to an engine via
// inproc and renders to os.Stdout in raw mode.
type TUI struct {
	Client   *inproc.Client
	ClientID ids.ClientID // matches Client.ID(); used to filter own TurnStarted echoes
	Session  ids.SessionID

	// Status line metadata, set by the caller before Run.
	Profile  string
	Model    string
	WebURL   string
	WebToken string

	mu       sync.Mutex
	width    int
	height   int
	out      io.Writer
	in       io.Reader
	oldState *term.State

	scrollback []string // logical lines; one per source line (no embedded \n)
	input      []rune
	status     string

	// assistantOpen tracks whether the last scrollback entry is the
	// in-progress assistant line that further TextDeltas should
	// continue. Reset when a new turn begins or a non-text event
	// arrives.
	assistantOpen bool

	// thinkingOpen mirrors assistantOpen for ThinkingDelta payloads.
	thinkingOpen bool

	// thinkingStartedAt is the wall-clock (UnixNano) when the
	// current thinking burst began. thinkingStartIdx is the
	// scrollback index of the first line of that burst, so we can
	// collapse the burst in place once a real reply starts.
	thinkingStartedAt int64
	thinkingStartIdx  int

	// scrollOffset is the number of visual rows the user has paged
	// back from the bottom. 0 == follow tail (default). Reset to 0
	// when the user submits input.
	scrollOffset int

	// spinner state for the "agent is working" indicator.
	// spinnerIdx is the scrollback index of the spinner row;
	// spinnerFrame cycles through spinnerFrames via the run-loop
	// ticker.
	spinnerIdx   int
	spinnerFrame int

	// search state — Ctrl-R toggles. While searchActive, keys go to
	// searchQuery (typed chars append, backspace shrinks, Esc cancels).
	// searchHits is the recomputed list of matching scrollback indices.
	searchActive bool
	searchQuery  string
	searchHits   []int

	// pendingPermission, when non-nil, indicates the engine is
	// waiting on a permit/deny decision. The next submit() consumes
	// the input as a permission answer rather than as new turn input.
	pendingPermission *control.PermissionRequested

	// inputHistory holds every non-empty submitted line in order.
	// historyIdx is the cursor when scrolling backward through it;
	// -1 means "off-history, showing fresh input." Recall fires on
	// Ctrl-P (back) and Ctrl-N (forward).
	inputHistory []string
	historyIdx   int

	// popupIdx is the highlighted entry in the slash-command popup.
	// Reset to 0 whenever the user mutates the input buffer. Up/Down
	// arrows move it; Tab completes to the highlighted name; Enter
	// submits the full command.
	popupIdx int

	// truncated tracks tool results that were clipped by
	// appendCappedMultiline. Ctrl-O pops the most-recent entry and
	// splices the elided source lines back into scrollback in place
	// of the "(+N lines)" hint.
	truncated []*truncatedSpan

	// modal, when non-nil, overlays a centered panel on top of the
	// scrollback and intercepts all keys. Set by /help and similar
	// info commands; cleared on Enter / Esc / q.
	modal *modalPanel

	// Cost meter shown in the status line. Updated by
	// CostIncremented events; rendered as "$0.0124" when non-zero.
	costUSD float64

	lastCtrlC int64 // unix nanoseconds of the last Ctrl-C press
}

// New constructs a TUI bound to the given inproc Client.
func New(c *inproc.Client, session ids.SessionID) *TUI {
	t := &TUI{
		Client:  c,
		Session: session,
		out:     os.Stdout,
		in:      os.Stdin,
	}
	if c != nil {
		t.ClientID = c.ID()
	}
	return t
}

// Run takes over the terminal, registers SIGWINCH/SIGINT/SIGTERM
// handlers, and pumps events + input until the user exits.
func (t *TUI) Run(ctx context.Context) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("tui: stdin is not a terminal; run with --no-tui or pipe input via gofastr harness --prompt")
	}
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	t.oldState = oldState
	defer t.restore()

	t.enterAltScreen()
	defer t.exitAltScreen()

	// Resize handler.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	t.readSize()

	t.showWelcome()

	// SIGINT/SIGTERM handler.
	sigQuit := make(chan os.Signal, 1)
	signal.Notify(sigQuit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigQuit)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Subscribe to engine events.
	events := t.Client.Subscribe(runCtx)

	// Input reader goroutine.
	keys := make(chan []byte, 16)
	go t.readKeys(runCtx, keys)

	// Spinner ticker — drives the "working…" frame animation while a
	// turn is in flight. 100ms is fast enough to look alive without
	// flooding the terminal.
	spinTick := time.NewTicker(100 * time.Millisecond)
	defer spinTick.Stop()

	t.draw()
	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-spinTick.C:
			t.mu.Lock()
			active := t.spinnerIdx >= 0 && t.spinnerIdx < len(t.scrollback) &&
				strings.HasPrefix(t.scrollback[t.spinnerIdx], spinnerLineMarker)
			if active {
				t.spinnerFrame++
			}
			t.mu.Unlock()
			if active {
				t.draw()
			}
		case <-winch:
			t.readSize()
			t.draw()
		case <-sigQuit:
			// Treat as Ctrl-C: cancel turn first; second within 2s exits.
			t.handleCtrlC()
		case env := <-events:
			t.renderEvent(env)
			t.draw()
		case bs, ok := <-keys:
			if !ok {
				return nil
			}
			if exit := t.handleKey(bs); exit {
				return nil
			}
			t.draw()
		}
	}
}

func (t *TUI) restore() {
	if t.oldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), t.oldState)
		t.oldState = nil
	}
}

func (t *TUI) enterAltScreen() {
	// Alt-screen + clear + cursor home + alternate scroll:
	//   ?1049h  alt screen — own buffer, restored on exit
	//   ?1007h  alternate scroll mode — the terminal translates
	//           scroll-wheel events into Up/Down arrow keys instead
	//           of sending mouse-button events to us. Crucially this
	//           leaves CLICK + DRAG owned by the terminal, so the
	//           user can still select text from the TUI to copy.
	//           Our existing arrow-key handler picks up the wheel
	//           scroll for free.
	_, _ = t.out.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H\x1b[?1007h"))
}

func (t *TUI) exitAltScreen() {
	// Reverse order: alt-scroll off, then leave alt-screen so the
	// underlying terminal returns to its normal state on exit.
	// Also defensively disable mouse tracking in case a future
	// profile ever turned it on.
	_, _ = t.out.Write([]byte("\x1b[?1007l\x1b[?1006l\x1b[?1000l\x1b[?1049l"))
}

func (t *TUI) readSize() {
	w, h, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		w, h = 80, 24
	}
	t.mu.Lock()
	t.width, t.height = w, h
	t.mu.Unlock()
}

func (t *TUI) readKeys(ctx context.Context, out chan<- []byte) {
	defer close(out)
	buf := make([]byte, 256)
	for {
		// Best-effort: when the context is done, the deferred restore
		// will close stdin's raw mode and the Read returns.
		if err := ctx.Err(); err != nil {
			return
		}
		n, err := t.in.Read(buf)
		if err != nil {
			return
		}
		// Copy the slice so the consumer owns it.
		cp := make([]byte, n)
		copy(cp, buf[:n])
		select {
		case out <- cp:
		case <-ctx.Done():
			return
		}
	}
}

// renderEvent appends a human-readable line to the scrollback for
// each interesting event.
func (t *TUI) renderEvent(env control.EventEnvelope) {
	e, err := control.DecodeEvent(env)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch v := e.(type) {
	case control.TextDelta:
		t.dismissSpinner()
		t.collapseThinkingBurst()
		t.ingestAssistantText(v.Text)
	case control.ThinkingDelta:
		t.dismissSpinner()
		t.ingestThinkingText(string(v.Block))
	case control.ToolCallStarted:
		t.dismissSpinner()
		t.collapseThinkingBurst()
		t.ensureBlankBefore()
		// Claude Code-style: `● Tool(args)` — filled circle bullet
		// followed by the function-call notation so args read like
		// code rather than a JSON dump.
		t.appendMultiline("● ", fmt.Sprintf("%s(%s)", v.Tool, summarizeArgs(v.Args)))
		t.assistantOpen = false
	case control.ToolCallProgress:
		t.appendMultiline("  ⎿ ", truncate(v.Partial, 400))
		t.assistantOpen = false
	case control.ToolResult:
		summary := strings.TrimSpace(summarizeContent(v.Content))
		if summary == "" {
			summary = "(no output)"
		}
		// `  ⎿ result` corner-connects to the preceding `● Tool(args)`
		// line. For errors we add an inline ✗ marker before the text
		// so the failure stands out visually.
		prefix := "  ⎿ "
		cap := 4 // truncate successful output to first N lines
		if v.IsError {
			prefix = "  ⎿ ✗ "
			cap = 8 // errors deserve more room
		}
		t.appendCappedMultiline(prefix, summary, cap)
		t.assistantOpen = false
	case control.TurnEnded:
		t.collapseThinkingBurst()
		// Visual separator between turns. The em-dash makes the
		// boundary obvious without taking a whole row of color.
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback,
			"── "+strings.Repeat("─", maxInt(0, t.width-gutterWidth-3)),
			"")
		t.assistantOpen = false
		t.thinkingOpen = false
	case control.Error:
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "[error] "+v.Message)
		t.assistantOpen = false
		t.thinkingOpen = false
	case control.CostIncremented:
		// Accumulator — every increment adds to the session total.
		t.costUSD += v.USD
	case control.TurnStarted:
		// Render the user message from OTHER clients (browser, MCP,
		// etc.). Self-originated TurnStarted is skipped because we
		// already optimistically echoed `→ msg` in submit().
		if t.ClientID == "" || env.Originator != t.ClientID {
			for _, b := range v.Content {
				if b.Type != "text" || b.Text == "" {
					continue
				}
				t.ensureBlankBefore()
				t.scrollback = append(t.scrollback, "→ "+b.Text)
				t.assistantOpen = false
			}
		}
		// Add a spinner row so the user sees the agent is working
		// even before the first thinking/text delta arrives. The row
		// is recorded with a marker constant; draw() substitutes the
		// current frame so it animates without re-pushing.
		t.ensureBlankBefore()
		t.spinnerIdx = len(t.scrollback)
		t.scrollback = append(t.scrollback, spinnerLineMarker+" working…")
	case control.PermissionRequested:
		t.ensureBlankBefore()
		t.appendMultiline(
			fmt.Sprintf("[permission] %s requested — ", v.Tool),
			summarizeArgs(v.Args))
		// Help line: indent under the start of the label content
		// (column-aligned with "B" of "Bash requested..."). `[permission] `
		// is 13 runes, so the indent is exactly 13 spaces.
		t.scrollback = append(t.scrollback,
			"             y = allow once, a = allow session, t = allow tool, enter/n = deny")
		// Stash a copy so submit() can route to AnswerPermission.
		copy := v
		t.pendingPermission = &copy
		t.assistantOpen = false
	}
	// Hard cap scrollback to last 1000 lines.
	if len(t.scrollback) > 1000 {
		t.scrollback = t.scrollback[len(t.scrollback)-1000:]
	}
}

// handleKey returns true if the TUI should exit.
func (t *TUI) handleKey(bs []byte) bool {
	if len(bs) == 0 {
		return false
	}
	// Modal absorbs all keys while active.
	t.mu.Lock()
	hasModal := t.modal != nil
	searchOn := t.searchActive
	t.mu.Unlock()
	if hasModal {
		t.modalKey(bs)
		return false
	}
	// Search mode absorbs typed chars + Esc/Backspace.
	if searchOn && len(bs) == 1 {
		c := bs[0]
		switch c {
		case 0x1b: // Esc — cancel search
			t.mu.Lock()
			t.deactivateSearch()
			t.mu.Unlock()
			return false
		case 0x0d: // Enter — accept (just leave search mode, keep hits)
			t.mu.Lock()
			t.searchActive = false
			t.mu.Unlock()
			return false
		case 0x7f, 0x08: // Backspace
			t.mu.Lock()
			if len(t.searchQuery) > 0 {
				t.searchQuery = t.searchQuery[:len(t.searchQuery)-1]
				t.updateSearch()
			}
			t.mu.Unlock()
			return false
		}
		if c >= 32 && c != 0x7f {
			t.mu.Lock()
			t.searchQuery += string(rune(c))
			t.updateSearch()
			t.mu.Unlock()
			return false
		}
	}
	// Single-byte handling.
	if len(bs) == 1 {
		c := bs[0]
		switch c {
		case 0x03: // Ctrl-C
			return t.handleCtrlC()
		case 0x04: // Ctrl-D (EOF on empty buffer)
			t.mu.Lock()
			empty := len(t.input) == 0
			t.mu.Unlock()
			if empty {
				return true
			}
			return false
		case 0x09: // Tab — slash-command completion
			t.handleTab()
			return false
		case 0x10: // Ctrl-P — previous in history
			t.recallHistory(-1)
			return false
		case 0x0e: // Ctrl-N — next in history
			t.recallHistory(+1)
			return false
		case 0x0f: // Ctrl-O — expand the last truncated tool result
			t.mu.Lock()
			t.expandLastTruncated()
			t.mu.Unlock()
			return false
		case 0x12: // Ctrl-R — incremental scrollback search
			t.mu.Lock()
			t.activateSearch()
			t.mu.Unlock()
			return false
		case 0x0a: // Ctrl-J / raw LF — insert newline (multi-line input)
			t.mu.Lock()
			t.input = append(t.input, '\n')
			t.popupIdx = 0
			t.mu.Unlock()
			return false
		case 0x0d: // Enter (CR) — submit OR popup-complete
			// If the slash popup is showing real completions (i.e.
			// there are candidates AND the current input isn't already
			// an exact candidate name), Enter behaves like Tab —
			// it completes the highlighted candidate. Otherwise Enter
			// submits as usual. This makes Enter symmetric with Tab
			// during autocomplete without ever blocking a real submit
			// when the user has already finished typing.
			if t.popupAcceptsEnter() {
				t.handleTab()
				return false
			}
			return t.submit()
		case 0x7f, 0x08: // Backspace
			t.mu.Lock()
			if len(t.input) > 0 {
				t.input = t.input[:len(t.input)-1]
			}
			t.popupIdx = 0
			t.mu.Unlock()
			return false
		}
		if c >= 32 && c != 0x7f {
			t.mu.Lock()
			t.input = append(t.input, rune(c))
			t.popupIdx = 0
			t.mu.Unlock()
			return false
		}
		return false
	}
	// Multi-byte: handle escape sequences (arrows, page nav, mouse) and UTF-8.
	if bs[0] == 0x1b {
		t.handleEscape(bs)
		return false
	}
	// UTF-8 multi-byte rune.
	t.mu.Lock()
	t.input = append(t.input, []rune(string(bs))...)
	t.mu.Unlock()
	return false
}

func (t *TUI) handleCtrlC() bool {
	now := nowUnixNano()
	// If we have a recent press, this is the second within the window → exit.
	if t.lastCtrlC > 0 && now-t.lastCtrlC < 2_000_000_000 {
		return true
	}
	t.lastCtrlC = now
	if t.Client != nil {
		_ = t.Client.Send(context.Background(), control.CancelTurn{SessionID: t.Session})
	}
	t.mu.Lock()
	t.scrollback = append(t.scrollback, "[ctrl-c] press again within 2s to exit")
	t.mu.Unlock()
	return false
}

// submit returns true when the TUI should exit (only set by local
// `/quit`).
func (t *TUI) submit() bool {
	t.mu.Lock()
	line := string(t.input)
	t.input = nil
	t.scrollOffset = 0 // typing returns the view to the tail
	t.assistantOpen = false
	pending := t.pendingPermission
	t.mu.Unlock()

	// Route to AnswerPermission when a permit prompt is pending.
	if pending != nil {
		decision, scope, label := decodePermissionAnswer(line)
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "→ "+label)
		t.pendingPermission = nil
		t.mu.Unlock()
		if t.Client != nil {
			_ = t.Client.Send(context.Background(), control.AnswerPermission{
				SessionID: t.Session,
				CallID:    pending.CallID,
				Decision:  decision,
				Scope:     scope,
			})
		}
		return false
	}

	if line == "" {
		return false
	}
	// Push to history (in-mu).
	t.mu.Lock()
	t.inputHistory = append(t.inputHistory, line)
	t.historyIdx = -1
	t.mu.Unlock()
	// Local-only slash commands (clear, help, web, quit) are handled
	// client-side without round-tripping through the engine. Anything
	// not claimed locally falls through to SendInput so server-side
	// handlers can take it.
	if strings.HasPrefix(line, "/") {
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "→ "+line)
		t.mu.Unlock()
		if handled, exit := t.dispatchLocalSlash(line); handled {
			return exit
		}
		// Fall through: forward to engine.
		if t.Client != nil {
			_ = t.Client.Send(context.Background(), control.SendInput{
				SessionID: t.Session,
				Content:   engine.SimpleInput(line),
			})
		}
		return false
	}

	t.mu.Lock()
	t.ensureBlankBefore()
	t.scrollback = append(t.scrollback, "→ "+line)
	t.mu.Unlock()
	if t.Client == nil {
		return false
	}
	_ = t.Client.Send(context.Background(), control.SendInput{
		SessionID: t.Session,
		Content:   engine.SimpleInput(line),
	})
	return false
}

// showWelcome appends a one-time banner to scrollback so the user
// has immediate context on what the binary is, the active model, and
// the keybinds. Idempotent: only fires when scrollback is empty.
func (t *TUI) showWelcome() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.scrollback) > 0 {
		return
	}
	model := t.Model
	if model == "" {
		model = "(no model)"
	}
	profile := t.Profile
	if profile == "" {
		profile = "(no profile)"
	}
	short := string(t.Session)
	if len(short) > 12 {
		short = short[:12] + "…"
	}
	t.scrollback = append(t.scrollback,
		ansiBold+"gofastr harness"+ansiReset,
		ansiDim+"model · "+model+"   profile · "+profile+"   session · "+short+ansiReset,
		"",
		ansiDim+"Tab           slash-command completion"+ansiReset,
		ansiDim+"Ctrl-P / N    input history"+ansiReset,
		ansiDim+"PgUp / Down   scroll  •  Home / End   top / tail"+ansiReset,
		ansiDim+"Ctrl-C × 2    exit  •  /help          list commands"+ansiReset,
		"",
	)
}

// recallHistory walks the input-history stack by delta (-1 = back,
// +1 = forward). Ctrl-P past the oldest entry stays at the oldest;
// Ctrl-N past the newest clears the input back to a blank prompt.
func (t *TUI) recallHistory(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.inputHistory)
	if n == 0 {
		return
	}
	switch {
	case t.historyIdx == -1 && delta < 0:
		t.historyIdx = n - 1
	case t.historyIdx == -1 && delta > 0:
		return // already at "fresh input"; no-op
	default:
		next := t.historyIdx + delta
		if next < 0 {
			next = 0
		}
		if next >= n {
			// Walking past the newest entry returns to a fresh prompt.
			t.input = nil
			t.historyIdx = -1
			return
		}
		t.historyIdx = next
	}
	t.input = []rune(t.inputHistory[t.historyIdx])
}

// handleTab performs slash-command completion. With a single match
// it inserts the full command. With multiple matches it inserts the
// candidate currently highlighted in the popup (Up/Down arrows to
// pick a different one). Trailing space appended for built-in names
// so the user can immediately type arguments.
func (t *TUI) handleTab() {
	t.mu.Lock()
	defer t.mu.Unlock()
	input := string(t.input)
	if !strings.HasPrefix(input, "/") {
		return
	}
	// Recompute the popup against the live input so we use the same
	// list the user sees.
	_, names, _ := slashPopupItems(input, 8)
	if len(names) == 0 {
		return
	}
	idx := t.popupIdx
	if idx < 0 || idx >= len(names) {
		idx = 0
	}
	chosen := names[idx]
	// Append a trailing space so the user types arguments directly.
	// Namespaces ending in `:` skip the space — they want a sub-verb.
	if !strings.HasSuffix(chosen, ":") {
		chosen += " "
	}
	t.input = []rune(chosen)
	t.popupIdx = 0
}

// decodePermissionAnswer maps the user's typed answer to a Decision +
// PermitScope pair. Empty input and any non-allow token default to
// deny (safe by default). Returns a human-readable label echoed to
// scrollback so the user sees what was sent.
func decodePermissionAnswer(line string) (control.Decision, control.PermitScope, string) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return control.DecisionAllow, control.ScopeOnce, "allow once"
	case "a", "all", "session":
		return control.DecisionAllow, control.ScopeSessionWide, "allow for this session"
	case "t", "tool":
		return control.DecisionAllow, control.ScopeTool, "allow this tool"
	default:
		return control.DecisionDeny, control.ScopeOnce, "deny"
	}
}

// ingestAssistantText handles a TextDelta payload. Splits on
// embedded newlines so each becomes its own scrollback line — the
// model often emits markdown structure (`\n## Heading\n\n- item\n`)
// in a single delta; rendering with embedded `\n` causes the terminal
// cursor to wander columns. assistantOpen tracks whether the next
// fragment continues the in-progress line or starts a new one.
//
// Continuation lines render flush-left (no indent prefix) so the
// model's own markdown formatting (headings, bullets) lands at
// column 0 the way the user expects.
func (t *TUI) ingestAssistantText(text string) {
	if text == "" {
		return
	}
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		if i == 0 {
			if t.assistantOpen && len(t.scrollback) > 0 {
				t.scrollback[len(t.scrollback)-1] += part
			} else {
				t.ensureBlankBefore()
				t.scrollback = append(t.scrollback, "← "+part)
				t.assistantOpen = true
			}
		} else {
			// Continuation lines have no prefix — let the model's
			// own indentation drive the layout.
			t.scrollback = append(t.scrollback, part)
		}
	}
	t.capScrollback()
}

// ingestThinkingText surfaces the model's reasoning content in a
// dimmed block. Same newline-splitting as ingestAssistantText.
//
// Records thinkingStartedAt + thinkingStartIdx on the first chunk so
// collapseThinkingBurst can later replace the whole burst with a
// single "* Cogitated for Xs" line.
func (t *TUI) ingestThinkingText(text string) {
	if text == "" || text == "null" {
		return
	}
	// json.RawMessage of a string arrives as `"..."` — unquote.
	if unquoted, ok := unjsonString(text); ok {
		text = unquoted
	}
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		if i == 0 {
			if t.thinkingOpen && len(t.scrollback) > 0 {
				t.scrollback[len(t.scrollback)-1] += part
			} else {
				t.ensureBlankBefore()
				t.thinkingStartedAt = nowUnixNano()
				t.thinkingStartIdx = len(t.scrollback)
				t.scrollback = append(t.scrollback, "… "+part)
				t.thinkingOpen = true
			}
		} else {
			t.scrollback = append(t.scrollback, part)
		}
	}
	t.capScrollback()
}

// dismissSpinner removes the "working…" placeholder once the first
// real event (text/thinking/tool) arrives. Idempotent.
//
// Caller must hold t.mu.
func (t *TUI) dismissSpinner() {
	if t.spinnerIdx < 0 || t.spinnerIdx >= len(t.scrollback) {
		return
	}
	if !strings.HasPrefix(t.scrollback[t.spinnerIdx], spinnerLineMarker) {
		return
	}
	t.scrollback = append(t.scrollback[:t.spinnerIdx], t.scrollback[t.spinnerIdx+1:]...)
	t.spinnerIdx = -1
}

// ensureBlankBefore appends a blank scrollback line if the last entry
// isn't already blank. Used to create visual breathing room between
// section boundaries (user → assistant → tool → result).
//
// Caller must hold t.mu.
func (t *TUI) ensureBlankBefore() {
	if len(t.scrollback) == 0 {
		return
	}
	if t.scrollback[len(t.scrollback)-1] == "" {
		return
	}
	t.scrollback = append(t.scrollback, "")
}

// appendMultiline appends `prefix + text` to scrollback, splitting
// text on embedded newlines so each becomes its own logical scrollback
// line (avoids the cursor-wander bug from bare LF in a raw-mode
// terminal). Continuation lines are indented to the same column as
// the first character after `prefix`, so multi-line tool output stays
// visually grouped under its marker.
//
// Caller must hold t.mu.
func (t *TUI) appendMultiline(prefix, text string) {
	t.appendCappedMultiline(prefix, text, 0)
}

// truncatedSpan tracks the elided portion of a capped tool result so
// Ctrl-O can re-expand it. fullLines is every source line (including
// the ones already shown); shownLines is how many landed in
// scrollback before the hint; hintIdx is the scrollback index of the
// "(+N lines)" hint line; indent is the continuation indent.
type truncatedSpan struct {
	fullLines  []string
	shownLines int
	hintIdx    int
	indent     string
}

// appendCappedMultiline is appendMultiline but truncates to the first
// `maxLines` source lines when text contains more, appending a dim
// "… (+N lines)" hint so the user knows output was elided. A
// maxLines of 0 means no cap (full output). Each truncated span is
// recorded so Ctrl-O can later splice the missing lines back in.
//
// Caller must hold t.mu.
func (t *TUI) appendCappedMultiline(prefix, text string, maxLines int) {
	parts := strings.Split(text, "\n")
	indent := strings.Repeat(" ", runeLen(prefix))
	n := len(parts)
	limit := n
	if maxLines > 0 && n > maxLines {
		limit = maxLines
	}
	for i := 0; i < limit; i++ {
		if i == 0 {
			t.scrollback = append(t.scrollback, prefix+parts[i])
		} else {
			t.scrollback = append(t.scrollback, indent+parts[i])
		}
	}
	if limit < n {
		hint := indent + fmt.Sprintf("… (+%d line", n-limit) + plural(n-limit, "s") + ", Ctrl-O to expand)"
		t.scrollback = append(t.scrollback, hint)
		t.truncated = append(t.truncated, &truncatedSpan{
			fullLines:  parts,
			shownLines: limit,
			hintIdx:    len(t.scrollback) - 1,
			indent:     indent,
		})
	}
	t.capScrollback()
}

// expandLastTruncated pops the most recent truncated tool result and
// splices the elided lines into scrollback in place of the
// "(+N lines)" hint. Other truncated spans' hintIdx values shift to
// account for the inserted lines. No-op if there's nothing to expand.
//
// Caller must hold t.mu.
func (t *TUI) expandLastTruncated() {
	if len(t.truncated) == 0 {
		return
	}
	last := t.truncated[len(t.truncated)-1]
	t.truncated = t.truncated[:len(t.truncated)-1]
	if last.hintIdx < 0 || last.hintIdx >= len(t.scrollback) {
		return
	}
	// Build expansion lines from shownLines..end.
	expansion := make([]string, 0, len(last.fullLines)-last.shownLines)
	for i := last.shownLines; i < len(last.fullLines); i++ {
		expansion = append(expansion, last.indent+last.fullLines[i])
	}
	// Splice: head + expansion + tail-after-hint.
	head := append([]string{}, t.scrollback[:last.hintIdx]...)
	tail := append([]string{}, t.scrollback[last.hintIdx+1:]...)
	t.scrollback = append(append(head, expansion...), tail...)
	// Shift remaining hint indices that lived after the spliced hint.
	delta := len(expansion) - 1 // expansion replaces the 1 hint line
	for _, sp := range t.truncated {
		if sp.hintIdx > last.hintIdx {
			sp.hintIdx += delta
		}
	}
}

func plural(n int, suffix string) string {
	if n == 1 {
		return ""
	}
	return suffix
}

// runeLen returns the display width of s in monospace cells, counting
// runes (so a multi-byte glyph like '▸' counts as 1). Good enough for
// our prefix indentation — not a substitute for east-asian-width logic.
func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// collapseThinkingBurst replaces an in-progress 🤔 block with a
// single "* Cogitated for Xs" line, matching the layout established
// CLI agents use to keep reasoning history tidy. No-op when there's
// no open burst.
func (t *TUI) collapseThinkingBurst() {
	if !t.thinkingOpen || t.thinkingStartedAt == 0 {
		return
	}
	elapsed := nowUnixNano() - t.thinkingStartedAt
	// Replace [thinkingStartIdx .. end] with a single summary line.
	if t.thinkingStartIdx < len(t.scrollback) {
		summary := "… " + cogitatedFor(elapsed)
		head := append([]string{}, t.scrollback[:t.thinkingStartIdx]...)
		t.scrollback = append(head, summary)
	}
	t.thinkingOpen = false
	t.thinkingStartedAt = 0
	t.thinkingStartIdx = 0
}

// cogitatedFor formats a duration as "cogitated for 8m 7s" / "12s".
// Lower-case to match the bracket-label aesthetic.
func cogitatedFor(nanos int64) string {
	if nanos <= 0 {
		return "thinking complete"
	}
	secs := nanos / 1_000_000_000
	if secs < 60 {
		return fmt.Sprintf("cogitated for %ds", secs)
	}
	return fmt.Sprintf("cogitated for %dm %ds", secs/60, secs%60)
}

// unjsonString tries to interpret a JSON-encoded string literal.
// Returns the unquoted value and true if successful.
func unjsonString(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s, false
	}
	var out string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return s, false
	}
	return out, true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (t *TUI) capScrollback() {
	if len(t.scrollback) > 5000 {
		t.scrollback = t.scrollback[len(t.scrollback)-5000:]
	}
}

// summarizeArgs renders a tool call's JSON args into a short
// human-readable summary for the scrollback line.
func summarizeArgs(raw []byte) string {
	s := string(raw)
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

// summarizeContent extracts the first text block from a tool result
// and truncates it for display. The limit is generous (4000 chars
// ≈ 50 lines at terminal width) so typical command output isn't cut
// mid-stream; the scrollback hard-cap of 5000 logical lines still
// protects memory from a runaway tool dump.
func summarizeContent(blocks []control.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return truncate(b.Text, 4000)
		}
	}
	return "(no text content)"
}

// wrapLine breaks a logical line into one or more visual lines of at
// most `width` runes. Word-aware: prefers to break at the last
// whitespace before `width` so words don't split mid-character.
// Falls back to a hard mid-word break only when a single token is
// longer than `width` (e.g. a long URL).
func wrapLine(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	if len(runes) <= width {
		return []string{s}
	}
	var out []string
	for len(runes) > width {
		// Find the last whitespace at or before `width`.
		breakAt := -1
		for i := width; i > 0; i-- {
			if isWrapBreak(runes[i-1]) {
				breakAt = i
				break
			}
		}
		if breakAt <= 0 {
			// No whitespace in the first `width` runes → hard break.
			breakAt = width
		}
		piece := string(runes[:breakAt])
		// Trim trailing whitespace from the visual line (avoids a
		// dangling space at line end).
		piece = trimTrailingSpaces(piece)
		out = append(out, piece)
		// Skip leading whitespace of the next line (we already used
		// it as the break point).
		runes = runes[breakAt:]
		for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t') {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

// isWrapBreak returns true for runes we're willing to break a line
// after. Whitespace is the natural primary choice; we also break
// after a small set of punctuation so a long URL containing slashes
// or dashes degrades gracefully.
func isWrapBreak(r rune) bool {
	switch r {
	case ' ', '\t':
		return true
	}
	return false
}

func trimTrailingSpaces(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// draw renders the screen. Full redraw per event; diff-based redraw
// is a future optimization.
//
// Layout:
//
//	row 0       : status line (reverse-video)
//	row 1..n-4  : scrollback (n-4 rows)
//	row n-3     : box top    ╭────────────╮
//	row n-2     : input row  │ > text     │
//	row n-1     : box bottom ╰────────────╯
//
// When the terminal is too short for a box (< 8 rows) we fall back to
// a single-row input prompt without the frame.
func (t *TUI) draw() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.width == 0 || t.height == 0 {
		return
	}
	// Clear + cursor home.
	_, _ = t.out.Write([]byte("\x1b[2J\x1b[H"))
	// Status line at row 0.
	status := t.statusLine()
	_, _ = fmt.Fprintf(t.out, "\x1b[7m%-*s\x1b[0m\r\n", t.width, truncate(status, t.width))

	boxed := t.width >= 24 && t.height >= 8
	inputRows := 1
	if boxed {
		inputRows = 3
	}

	// Slash-command popup: live-filtered candidate list shown right
	// above the input box while the user is typing `/…`. Eats into
	// the scrollback area; capped so it never takes more than half
	// the available vertical space.
	var popupItems []string
	var popupNames []string
	maxPopup := (t.height - 1 - inputRows) / 2
	if maxPopup > 8 {
		maxPopup = 8
	}
	if maxPopup < 2 {
		maxPopup = 0 // not enough room — skip the popup on tiny screens
	}
	if t.modal == nil && maxPopup > 0 {
		popupItems, popupNames, _ = slashPopupItems(string(t.input), maxPopup)
	}
	// Clamp popupIdx to the selectable range (names slice). The
	// overflow hint sits past len(popupNames) and isn't selectable.
	if t.popupIdx < 0 {
		t.popupIdx = 0
	}
	if len(popupNames) > 0 && t.popupIdx >= len(popupNames) {
		t.popupIdx = len(popupNames) - 1
	}
	popupRows := len(popupItems)

	scrollRows := t.height - 1 - inputRows - popupRows
	if scrollRows < 0 {
		scrollRows = 0
	}

	// Flatten logical scrollback into visual rows. Wrap width is
	// reduced by gutterWidth so the gutter space doesn't push
	// content off the right edge.
	contentWidth := t.width - gutterWidth
	if contentWidth < 10 {
		contentWidth = 10
	}
	visual := make([]string, 0, len(t.scrollback)*2)
	for _, line := range t.scrollback {
		if strings.HasPrefix(line, spinnerLineMarker) {
			line = string(spinnerGlyph(t.spinnerFrame)) + line[len(spinnerLineMarker):]
		}
		visual = append(visual, wrapLine(line, contentWidth)...)
	}

	// Clamp scrollOffset to [0, len(visual)-scrollRows] so Home key
	// can be a "go-to-top" by setting offset to a huge number.
	maxOffset := len(visual) - scrollRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.scrollOffset > maxOffset {
		t.scrollOffset = maxOffset
	}
	end := len(visual) - t.scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - scrollRows
	if start < 0 {
		start = 0
	}
	for i := start; i < end; i++ {
		styled := renderMarkdownInline(colorizeMarker(visual[i]))
		_, _ = fmt.Fprintf(t.out, "%s%s\r\n", gutter(), styled)
	}
	// Pad remaining rows.
	for i := end - start; i < scrollRows; i++ {
		_, _ = t.out.Write([]byte("\r\n"))
	}
	// Slash-command popup, just above the input box. Selected row is
	// rendered with reverse video so the user knows which name Tab
	// will insert; non-selected rows are dim.
	for i, row := range popupItems {
		style := ansiDim
		if i == t.popupIdx && i < len(popupNames) {
			style = ansiReverse
		}
		_, _ = fmt.Fprintf(t.out, "%s%s%s%s\r\n",
			gutter(), style, row, ansiReset)
	}
	t.drawInput(boxed)
	// Modal goes last so it covers scrollback + input.
	t.drawModal()
}

// drawInput renders the input area at the bottom of the screen. When
// boxed is true, draws a 3-row rounded box; otherwise a single-row
// prompt line for very small terminals. The cursor naturally lands
// after the last byte written, which is the end of the input string,
// so no explicit cursor-position escape is needed.
func (t *TUI) drawInput(boxed bool) {
	indicator := ""
	if t.scrollOffset > 0 {
		indicator = fmt.Sprintf("[scrolled -%d, End to follow]  ", t.scrollOffset)
	}
	prompt := "> "
	if t.pendingPermission != nil {
		prompt = "permit? [y/a/t/N] "
	}
	if !boxed {
		_, _ = fmt.Fprintf(t.out, "%s%s%s%s%s%s%s",
			gutter(), ansiBold, prompt, ansiReset,
			ansiDim+indicator+ansiReset,
			string(t.input), ansiReset)
		return
	}
	// Box geometry: starts at the gutter column, spans to t.width-1.
	// Inside width = t.width - gutterWidth - 4 (two `│` chars + two
	// spaces of inner padding).
	boxLeft := gutter()
	frameWidth := t.width - gutterWidth // total width of the box
	if frameWidth < 8 {
		frameWidth = 8
	}
	innerWidth := frameWidth - 4 // 2 frame chars + 2 inner pad spaces
	if innerWidth < 4 {
		innerWidth = 4
	}
	// Top border.
	_, _ = fmt.Fprintf(t.out, "%s%s╭%s╮%s\r\n",
		boxLeft, ansiDim,
		strings.Repeat("─", frameWidth-2),
		ansiReset)
	// Middle (input) row.
	visible := string(t.input)
	// If the input runs longer than the inner width, show a left-side
	// ellipsis so the cursor (end of input) stays visible.
	if runeLen(prompt)+runeLen(indicator)+runeLen(visible) > innerWidth {
		overflow := runeLen(prompt) + runeLen(indicator) + runeLen(visible) - innerWidth + 1
		runes := []rune(visible)
		if overflow < len(runes) {
			visible = "…" + string(runes[overflow:])
		}
	}
	inner := fmt.Sprintf("%s%s%s%s%s%s",
		ansiBold, prompt, ansiReset,
		ansiDim+indicator+ansiReset,
		visible, ansiReset)
	innerVisibleLen := runeLen(prompt) + runeLen(indicator) + runeLen(visible)
	pad := innerWidth - innerVisibleLen
	if pad < 0 {
		pad = 0
	}
	_, _ = fmt.Fprintf(t.out, "%s%s│%s %s%s %s│%s\r\n",
		boxLeft, ansiDim, ansiReset,
		inner, strings.Repeat(" ", pad),
		ansiDim, ansiReset)
	// Bottom border.
	_, _ = fmt.Fprintf(t.out, "%s%s╰%s╯%s",
		boxLeft, ansiDim,
		strings.Repeat("─", frameWidth-2),
		ansiReset)
	// Move cursor back into the input row so the user types into the
	// box instead of past the bottom border. Columns are 1-indexed:
	//   gutterWidth + `│` + space + prompt + indicator + input.
	cursorRow := t.height - 1
	cursorCol := gutterWidth + 2 + runeLen(prompt) + runeLen(indicator) + runeLen(visible) + 1
	_, _ = fmt.Fprintf(t.out, "\x1b[%d;%dH", cursorRow, cursorCol)
}

func (t *TUI) statusLine() string {
	web := ""
	if t.WebURL != "" {
		web = "  •  web: " + t.WebURL
	}
	cost := ""
	if t.costUSD > 0 {
		cost = fmt.Sprintf("  •  $%.4f", t.costUSD)
	}
	return fmt.Sprintf(" gofastr harness  •  %s  •  %s%s%s", t.Profile, t.Model, cost, web)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

// nowFn is swappable in tests.
var nowFn = func() int64 {
	return timeNowUnixNano()
}

func nowUnixNano() int64 { return nowFn() }
