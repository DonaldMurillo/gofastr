package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TUI is the bundled terminal client: it attaches to an engine through inproc and renders to stdout in raw mode.
type TUI struct {
	Client   *inproc.Client
	ClientID ids.ClientID // Client.ID(); its own TurnStarted echoes are not drawn again
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

	scrollback []string
	input      []rune
	status     string

	assistantOpen bool

	thinkingOpen bool

	thinkingStartedAt int64
	thinkingStartIdx  int

	scrollOffset int

	spinnerIdx   int
	spinnerFrame int

	searchActive bool
	searchQuery  string
	searchHits   []int

	pendingPermissions []control.PermissionRequested

	inputHistory []string
	historyIdx   int

	popupIdx int

	truncated []*truncatedSpan

	modal *modalPanel

	costUSD float64

	lastCtrlC int64
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

// Run takes over the terminal and pumps events and keys until the user exits.
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

	winch := make(chan os.Signal, 1)
	notifyWindowResize(winch)
	defer signal.Stop(winch)
	t.readSize()

	t.showWelcome()

	sigQuit := make(chan os.Signal, 1)
	signal.Notify(sigQuit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigQuit)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := t.Client.Subscribe(runCtx)

	keys := make(chan []byte, 16)
	go t.readKeys(runCtx, keys)

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
	_, _ = t.out.Write([]byte("\x1b[?1049h\x1b[2J\x1b[H\x1b[?1007h"))
}

func (t *TUI) exitAltScreen() {
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
		if err := ctx.Err(); err != nil {
			return
		}
		n, err := t.in.Read(buf)
		if err != nil {
			return
		}
		cp := make([]byte, n)
		copy(cp, buf[:n])
		select {
		case out <- cp:
		case <-ctx.Done():
			return
		}
	}
}

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
		t.ingestAssistantText(textsafe.ScrubTerminal(v.Text))
	case control.ThinkingDelta:
		t.dismissSpinner()
		t.ingestThinkingText(string(v.Block))
	case control.ToolCallStarted:
		t.dismissSpinner()
		t.collapseThinkingBurst()
		t.ensureBlankBefore()
		t.appendMultiline("● ", fmt.Sprintf("%s(%s)", textsafe.ScrubTerminal(v.Tool), textsafe.ScrubTerminal(summarizeArgs(v.Args))))
		t.assistantOpen = false
	case control.ToolCallProgress:
		t.appendMultiline("  ⎿ ", truncate(textsafe.ScrubTerminal(v.Partial), 400))
		t.assistantOpen = false
	case control.ToolResult:
		t.dropPermission(v.CallID)
		summary := strings.TrimSpace(textsafe.ScrubTerminal(summarizeContent(v.Content)))
		if summary == "" {
			summary = "(no output)"
		}
		prefix := "  ⎿ "
		cap := 4
		if v.IsError {
			prefix = "  ⎿ ✗ "
			cap = 8
		}
		t.appendCappedMultiline(prefix, summary, cap)
		t.assistantOpen = false
	case control.TurnEnded:
		t.pendingPermissions = nil
		t.collapseThinkingBurst()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback,
			"── "+strings.Repeat("─", maxInt(0, t.width-gutterWidth-3)),
			"")
		t.assistantOpen = false
		t.thinkingOpen = false
	case control.Error:
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "[error] "+textsafe.ScrubTerminal(v.Message))
		t.assistantOpen = false
		t.thinkingOpen = false
	case control.Cancelled, control.SessionEnded:
		t.pendingPermissions = nil
	case control.CostIncremented:
		t.costUSD += v.USD
	case control.TurnStarted:
		if t.ClientID == "" || env.Originator != t.ClientID {
			for _, b := range v.Content {
				if b.Type != "text" || b.Text == "" {
					continue
				}
				t.ensureBlankBefore()
				t.scrollback = append(t.scrollback, "→ "+textsafe.ScrubTerminal(b.Text))
				t.assistantOpen = false
			}
		}
		t.ensureBlankBefore()
		t.spinnerIdx = len(t.scrollback)
		t.scrollback = append(t.scrollback, spinnerLineMarker+" working…")
	case control.PermissionRequested:
		t.ensureBlankBefore()
		t.appendMultiline(
			fmt.Sprintf("[permission] %s requested — ", textsafe.ScrubTerminal(v.Tool)),
			textsafe.ScrubTerminal(summarizeArgs(v.Args)))
		t.scrollback = append(t.scrollback,
			"             y = allow once, a = allow session, t = allow tool, enter/n = deny")
		t.dropPermission(v.CallID)
		t.pendingPermissions = append(t.pendingPermissions, v)
		t.assistantOpen = false
	}
	t.capScrollback()
}

func (t *TUI) handleKey(bs []byte) bool {
	if len(bs) == 0 {
		return false
	}
	t.mu.Lock()
	hasModal := t.modal != nil
	searchOn := t.searchActive
	t.mu.Unlock()
	if hasModal {
		t.modalKey(bs)
		return false
	}
	if searchOn && len(bs) == 1 {
		c := bs[0]
		switch c {
		case 0x1b:
			t.mu.Lock()
			t.deactivateSearch()
			t.mu.Unlock()
			return false
		case 0x0d:
			t.mu.Lock()
			t.searchActive = false
			t.mu.Unlock()
			return false
		case 0x7f, 0x08:
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
	if len(bs) == 1 {
		c := bs[0]
		switch c {
		case 0x03:
			return t.handleCtrlC()
		case 0x04:
			t.mu.Lock()
			empty := len(t.input) == 0
			t.mu.Unlock()
			if empty {
				return true
			}
			return false
		case 0x09:
			t.handleTab()
			return false
		case 0x10:
			t.recallHistory(-1)
			return false
		case 0x0e:
			t.recallHistory(+1)
			return false
		case 0x0f:
			t.mu.Lock()
			t.expandLastTruncated()
			t.mu.Unlock()
			return false
		case 0x12:
			t.mu.Lock()
			t.activateSearch()
			t.mu.Unlock()
			return false
		case 0x0a:
			t.mu.Lock()
			t.input = append(t.input, '\n')
			t.popupIdx = 0
			t.mu.Unlock()
			return false
		case 0x0d:
			if t.popupAcceptsEnter() {
				t.handleTab()
				return false
			}
			return t.submit()
		case 0x7f, 0x08:
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
	if bs[0] == 0x1b {
		t.handleEscape(bs)
		return false
	}
	t.mu.Lock()
	t.input = append(t.input, []rune(string(bs))...)
	t.mu.Unlock()
	return false
}

func (t *TUI) handleCtrlC() bool {
	now := nowUnixNano()
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

func (t *TUI) submit() bool {
	t.mu.Lock()
	line := string(t.input)
	t.input = nil
	t.scrollOffset = 0
	t.assistantOpen = false
	var pending *control.PermissionRequested
	if len(t.pendingPermissions) > 0 {
		head := t.pendingPermissions[0]
		pending = &head
	}
	t.mu.Unlock()

	if pending != nil {
		decision, scope, label := decodePermissionAnswer(line)
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "→ "+label)
		t.dropPermission(pending.CallID)
		if len(t.pendingPermissions) > 0 {
			t.scrollback = append(t.scrollback,
				fmt.Sprintf("[permission] next: %s", textsafe.ScrubTerminal(t.pendingPermissions[0].Tool)))
		}
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
	t.mu.Lock()
	t.inputHistory = append(t.inputHistory, line)
	t.historyIdx = -1
	t.mu.Unlock()
	if strings.HasPrefix(line, "/") {
		t.mu.Lock()
		t.ensureBlankBefore()
		t.scrollback = append(t.scrollback, "→ "+line)
		t.mu.Unlock()
		if handled, exit := t.dispatchLocalSlash(line); handled {
			return exit
		}
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
		return
	default:
		next := max(t.historyIdx+delta, 0)
		if next >= n {
			t.input = nil
			t.historyIdx = -1
			return
		}
		t.historyIdx = next
	}
	t.input = []rune(t.inputHistory[t.historyIdx])
}

func (t *TUI) handleTab() {
	t.mu.Lock()
	defer t.mu.Unlock()
	input := string(t.input)
	if !strings.HasPrefix(input, "/") {
		return
	}
	_, names, _ := slashPopupItems(input, 8)
	if len(names) == 0 {
		return
	}
	idx := t.popupIdx
	if idx < 0 || idx >= len(names) {
		idx = 0
	}
	chosen := names[idx]
	if !strings.HasSuffix(chosen, ":") {
		chosen += " "
	}
	t.input = []rune(chosen)
	t.popupIdx = 0
}

func (t *TUI) dropPermission(id ids.CallID) {
	t.pendingPermissions = slices.DeleteFunc(t.pendingPermissions, func(p control.PermissionRequested) bool {
		return p.CallID == id
	})
}

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
			t.scrollback = append(t.scrollback, part)
		}
	}
	t.capScrollback()
}

func (t *TUI) ingestThinkingText(text string) {
	if text == "" || text == "null" {
		return
	}
	if unquoted, ok := unjsonString(text); ok {
		text = unquoted
	}
	text = textsafe.ScrubTerminal(text)
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

func (t *TUI) ensureBlankBefore() {
	if len(t.scrollback) == 0 {
		return
	}
	if t.scrollback[len(t.scrollback)-1] == "" {
		return
	}
	t.scrollback = append(t.scrollback, "")
}

func (t *TUI) appendMultiline(prefix, text string) {
	t.appendCappedMultiline(prefix, text, 0)
}

type truncatedSpan struct {
	fullLines  []string
	shownLines int
	hintIdx    int
	indent     string
}

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

func (t *TUI) expandLastTruncated() {
	if len(t.truncated) == 0 {
		return
	}
	last := t.truncated[len(t.truncated)-1]
	t.truncated = t.truncated[:len(t.truncated)-1]
	if last.hintIdx < 0 || last.hintIdx >= len(t.scrollback) {
		return
	}
	expansion := make([]string, 0, len(last.fullLines)-last.shownLines)
	for i := last.shownLines; i < len(last.fullLines); i++ {
		expansion = append(expansion, last.indent+last.fullLines[i])
	}
	head := append([]string{}, t.scrollback[:last.hintIdx]...)
	tail := append([]string{}, t.scrollback[last.hintIdx+1:]...)
	t.scrollback = append(append(head, expansion...), tail...)
	delta := len(expansion) - 1
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

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func (t *TUI) collapseThinkingBurst() {
	if !t.thinkingOpen || t.thinkingStartedAt == 0 {
		return
	}
	elapsed := nowUnixNano() - t.thinkingStartedAt
	if t.thinkingStartIdx < len(t.scrollback) {
		summary := "… " + cogitatedFor(elapsed)
		head := append([]string{}, t.scrollback[:t.thinkingStartIdx]...)
		t.scrollback = append(head, summary)
	}
	t.thinkingOpen = false
	t.thinkingStartedAt = 0
	t.thinkingStartIdx = 0
}

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

const maxScrollback = 1000

func (t *TUI) capScrollback() {
	n := len(t.scrollback) - maxScrollback
	if n <= 0 {
		return
	}
	t.scrollback = t.scrollback[n:]
	if t.spinnerIdx >= n {
		t.spinnerIdx -= n
	} else {
		t.spinnerIdx = -1
	}
	t.thinkingStartIdx = max(t.thinkingStartIdx-n, 0)
	kept := t.truncated[:0]
	for _, sp := range t.truncated {
		if sp.hintIdx -= n; sp.hintIdx >= 0 {
			kept = append(kept, sp)
		}
	}
	t.truncated = kept
}

func summarizeArgs(raw []byte) string {
	s := string(raw)
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

func summarizeContent(blocks []control.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return truncate(b.Text, 4000)
		}
	}
	return "(no text content)"
}

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
		breakAt := -1
		for i := width; i > 0; i-- {
			if isWrapBreak(runes[i-1]) {
				breakAt = i
				break
			}
		}
		if breakAt <= 0 {
			breakAt = width
		}
		piece := string(runes[:breakAt])
		piece = trimTrailingSpaces(piece)
		out = append(out, piece)
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

func (t *TUI) draw() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.width == 0 || t.height == 0 {
		return
	}
	_, _ = t.out.Write([]byte("\x1b[2J\x1b[H"))
	status := t.statusLine()
	_, _ = fmt.Fprintf(t.out, "\x1b[7m%-*s\x1b[0m\r\n", t.width, truncate(status, t.width))

	boxed := t.width >= 24 && t.height >= 8
	inputRows := 1
	if boxed {
		inputRows = 3
	}

	var popupItems []string
	var popupNames []string
	maxPopup := min((t.height-1-inputRows)/2, 8)
	if maxPopup < 2 {
		maxPopup = 0
	}
	if t.modal == nil && maxPopup > 0 {
		popupItems, popupNames, _ = slashPopupItems(string(t.input), maxPopup)
	}
	if t.popupIdx < 0 {
		t.popupIdx = 0
	}
	if len(popupNames) > 0 && t.popupIdx >= len(popupNames) {
		t.popupIdx = len(popupNames) - 1
	}
	popupRows := len(popupItems)

	scrollRows := max(t.height-1-inputRows-popupRows, 0)

	contentWidth := max(t.width-gutterWidth, 10)
	visual := make([]string, 0, len(t.scrollback)*2)
	code := make([]bool, 0, len(t.scrollback)*2)
	var fence fenceTracker
	for _, line := range t.scrollback {
		if strings.HasPrefix(line, spinnerLineMarker) {
			line = string(spinnerGlyph(t.spinnerFrame)) + line[len(spinnerLineMarker):]
		}
		inCode := fence.next(line)
		for _, row := range wrapLine(line, contentWidth) {
			visual = append(visual, row)
			code = append(code, inCode)
		}
	}

	maxOffset := max(len(visual)-scrollRows, 0)
	if t.scrollOffset > maxOffset {
		t.scrollOffset = maxOffset
	}
	end := max(len(visual)-t.scrollOffset, 0)
	start := max(end-scrollRows, 0)
	for i := start; i < end; i++ {
		styled := colorizeMarker(visual[i])
		if !code[i] {
			styled = renderMarkdownInline(styled)
		}
		_, _ = fmt.Fprintf(t.out, "%s%s\r\n", gutter(), styled)
	}
	for i := end - start; i < scrollRows; i++ {
		_, _ = t.out.Write([]byte("\r\n"))
	}
	for i, row := range popupItems {
		style := ansiDim
		if i == t.popupIdx && i < len(popupNames) {
			style = ansiReverse
		}
		_, _ = fmt.Fprintf(t.out, "%s%s%s%s\r\n",
			gutter(), style, row, ansiReset)
	}
	t.drawInput(boxed)
	t.drawModal()
}

func (t *TUI) drawInput(boxed bool) {
	indicator := ""
	if t.scrollOffset > 0 {
		indicator = fmt.Sprintf("[scrolled -%d, End to follow]  ", t.scrollOffset)
	}
	prompt := "> "
	if len(t.pendingPermissions) > 0 {
		prompt = "permit? [y/a/t/N] "
	}
	if !boxed {
		_, _ = fmt.Fprintf(t.out, "%s%s%s%s%s%s%s",
			gutter(), ansiBold, prompt, ansiReset,
			ansiDim+indicator+ansiReset,
			string(t.input), ansiReset)
		return
	}
	boxLeft := gutter()
	frameWidth := max(
		t.width-gutterWidth, 8)
	innerWidth := max(
		frameWidth-4, 4)
	_, _ = fmt.Fprintf(t.out, "%s%s╭%s╮%s\r\n",
		boxLeft, ansiDim,
		strings.Repeat("─", frameWidth-2),
		ansiReset)
	visible := string(t.input)
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
	pad := max(innerWidth-innerVisibleLen, 0)
	_, _ = fmt.Fprintf(t.out, "%s%s│%s %s%s %s│%s\r\n",
		boxLeft, ansiDim, ansiReset,
		inner, strings.Repeat(" ", pad),
		ansiDim, ansiReset)
	_, _ = fmt.Fprintf(t.out, "%s%s╰%s╯%s",
		boxLeft, ansiDim,
		strings.Repeat("─", frameWidth-2),
		ansiReset)
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
	cut := max - 1
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

var nowFn = func() int64 {
	return timeNowUnixNano()
}

func nowUnixNano() int64 { return nowFn() }
