package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// agentTurnWaitDelay bounds cmd.Wait once the adapter child has exited
// or the turn's context is done but its stdout pipe is still held open
// by a descendant the adapter forked and did not reap. Same shape and
// rationale as codegen's extensionWaitDelay and the harness bash tool.
var agentTurnWaitDelay = 2 * time.Second

// agentShutdownWait bounds how long runAgentWatcher waits, once its
// context ends, for in-flight turns to finish: the group kill, the
// WaitDelay-bounded drain and the closing journal note. It sits above
// agentTurnWaitDelay so a turn whose pipe is held still gets its drain.
var agentShutdownWait = agentTurnWaitDelay + 3*time.Second

// maxAgentStdout caps the captured adapter stdout: an adapter that
// streams unbounded output must not grow the kiln process without
// bound. 4 MiB is far above any real turn reply (they render in a chat
// bubble); past it the capture truncates silently.
const maxAgentStdout = 4 << 20

// cappedBuffer accumulates at most limit bytes and silently discards
// the rest, recording that it did. Discarding beats erroring from
// Write: cmd.Run would kill the child mid-stream and report the write
// error instead of the turn's reply. Same shape as codegen's
// cappedBuffer (unexported there, so the twin is local).
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) <= room {
			return b.buf.Write(p)
		}
		b.buf.Write(p[:room])
	}
	b.truncated = true
	return len(p), nil // report full length so the writer never errors
}

func (b *cappedBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *cappedBuffer) Len() int      { return b.buf.Len() }

// countToolsAndErrors snapshots the journaled tool counts so the
// per-turn summary at agent_turn_ended is just (post - pre) without
// needing to track via mutex during the run.
func countToolsAndErrors(l *live.Live) (calls, errors int) {
	// Read under Live's session lock: the HTTP tool dispatcher Applies
	// concurrently with the watcher loop.
	l.ReadSession(func(sess *journal.Session) {
		for _, e := range sess.Chat {
			if e.Kind == journal.KindToolCall {
				calls++
			}
			if e.Kind == journal.KindToolResult && e.Result != nil && !e.Result.OK {
				errors++
			}
		}
	})
	return
}

// turnSummary renders a one-line system note appended at the end of
// every agent turn so the user gets visible closure (vs. "did pi
// stop or is it still working?"). Examples:
//
//	"✓ turn complete · 5 tools · 23s"
//	"⚠ turn complete · 11 tools · 2 errors · 47s"
//	"· no tools used · 1.2s" (chat-only reply)
func turnSummary(toolCount, errorCount int, dur time.Duration) string {
	prefix := "✓"
	if errorCount > 0 {
		prefix = "⚠"
	}
	parts := []string{prefix + " turn complete"}
	if toolCount == 0 {
		parts = []string{"· no tools used"}
	} else {
		if toolCount == 1 {
			parts = append(parts, "1 tool")
		} else {
			parts = append(parts, fmt.Sprintf("%d tools", toolCount))
		}
		if errorCount > 0 {
			if errorCount == 1 {
				parts = append(parts, "1 error")
			} else {
				parts = append(parts, fmt.Sprintf("%d errors", errorCount))
			}
		}
	}
	parts = append(parts, formatTurnDuration(dur))
	return "[" + strings.Join(parts, " · ") + "]"
}

func formatTurnDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// runAgentWatcher subscribes to live's event bus and spawns the
// configured agent command once per chat_user event. KILN_URL is
// injected so the spawned process can drive the runtime via HTTP. The
// command's stdout is captured and journaled as chat_assistant so the
// floating panel renders the agent's reply.
//
// Spawn semantics: cancel-and-replace. A second chat_user that arrives
// while the first turn is still running CANCELS the first via context
// cancellation (which kills the subprocess tree on Unix) and starts the
// new one. The cancelled turn journals a synthetic "(superseded …)"
// note so the panel's thinking indicator clears and the user sees what
// happened. Anything the cancelled turn already wrote to the journal
// stays; Kiln's world is append-only, so partial work persists.
//
// It returns only after its turns have ended (bounded by
// agentShutdownWait). The turn's process-group kill runs on exec's own
// goroutine, and the adapter has its own group, so the terminal's
// SIGINT never reaches it: returning at once let kiln serve exit with
// the agent CLI still running and editing the checkout.
func runAgentWatcher(ctx context.Context, logger *log.Logger, l *live.Live, tools *protocol.Tools, store *AdapterStore, addr string) {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	kilnURL := "http://" + host

	ch, unsub := l.Subscribe()
	defer unsub()

	var turns sync.WaitGroup
	defer func() {
		// Process exit (or a closed bus): cancel any in-flight turn
		// through the store, then wait for it to be killed and reaped.
		store.Set(store.Get())
		waitTurns(&turns, agentShutdownWait, logger)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Kind != string(journal.KindChatUser) {
				continue
			}
			text, acpSession := chatTextByEntryID(l, ev.EntryID)
			// An ACP session's message is that session's turn: kiln acp
			// runs it on its own provider or answers it refused, and a
			// watcher turn on it would run a prompt the client was told
			// was refused.
			if text == "" || acpSession != "" {
				continue
			}

			// Read the current adapter at turn-spawn time so runtime
			// switches via /kiln/agent take effect on the very next turn.
			adapter := store.Get()
			if adapter.BuildArgs == nil {
				continue
			}

			turnCtx, cancel := context.WithCancelCause(ctx)
			// Register cancel with the store. If a prior turn was still
			// running, the store cancels it with errSupersededByNewMessage.
			// The returned token is this turn's ownership proof: when THIS
			// turn drains, its clear only takes effect if no newer turn (or
			// agent switch) has since taken the slot.
			reg := store.SetTurnCancel(cancel)

			turns.Add(1)
			go func(turnCtx context.Context, cancel context.CancelCauseFunc, reg *turnCancel, text string, adapter Adapter) {
				defer turns.Done()
				defer cancel(nil)
				l.Notify("agent_turn_started", adapter.Name)
				start := time.Now()
				preToolCount, preErrCount := countToolsAndErrors(l)
				runOneAgentTurn(turnCtx, logger, tools, adapter, kilnURL, text)
				store.ClearTurnCancel(reg)
				postToolCount, postErrCount := countToolsAndErrors(l)
				_ = tools.Chat(context.Background(), protocol.ChatArgs{
					Role: "assistant",
					Text: turnSummary(postToolCount-preToolCount, postErrCount-preErrCount, time.Since(start)),
				})
				l.Notify("agent_turn_ended", adapter.Name)
			}(turnCtx, cancel, reg, text, adapter)
		}
	}
}

// waitTurns waits for turns to finish, up to limit.
func waitTurns(turns *sync.WaitGroup, limit time.Duration, logger *log.Logger) {
	done := make(chan struct{})
	go func() {
		turns.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(limit):
		logger.Printf("agent: a turn was still ending after %v; exiting without it", limit)
	}
}

// adapterDirOpened runs between claimAdapterDir's open and its chmod.
// Tests swap the name there.
var adapterDirOpened = func() {}

// claimAdapterDir tightens an adapter's fixed work dir to 0700, or
// refuses it. The name must be a real directory: chmod by path follows
// a symlink, so a co-user's link to a directory this user owns (a
// ~/public_html) got that directory tightened and the turn ran inside
// it. Checking with Lstat first still left a window: a co-user who
// planted a real directory could swap the name for such a link before
// the chmod. So the dir is opened without following a link, the handle
// is chmodded (only its owner can, so a co-user's dir is refused, not
// adopted), and the name must still be that directory afterwards.
func claimAdapterDir(dir string) error {
	f, err := openNoFollowDir(dir)
	if err != nil {
		return fmt.Errorf("not a directory (a symlink is refused): %w", err)
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil || !held.IsDir() {
		return errors.New("not a directory")
	}
	adapterDirOpened()
	if err := f.Chmod(0o700); err != nil {
		return fmt.Errorf("not owned by this user (chmod 0700: %w)", err)
	}
	if now, err := os.Lstat(dir); err != nil || !os.SameFile(held, now) {
		return errors.New("the name was replaced while it was being claimed")
	}
	return nil
}

// chatTextByEntryID is a chat message's text and the ACP session that
// wrote it, "" for the panel and the HTTP and MCP tools.
func chatTextByEntryID(l *live.Live, id string) (text, acpSession string) {
	// Read under Live's session lock: the watcher goroutine competes
	// with the HTTP tool dispatcher's Applies for the chat slice.
	l.ReadSession(func(sess *journal.Session) {
		for _, e := range sess.Chat {
			if e.EntryID == id && e.Message != nil {
				text, acpSession = e.Message.Text, e.Message.ACPSession
				return
			}
		}
	})
	return text, acpSession
}

// destructiveIntent runs a quick keyword scan on the user message
// before the selected agent sees it. If the user asked for something destructive
// (delete, drop, wipe, rebuild, reset) we prepend a directive forcing
// the agent through propose_plan + user approval first. This is a
// pre-flight classifier: cheap, deterministic, no extra LLM call.
func destructiveIntent(text string) bool {
	low := strings.ToLower(text)
	for _, kw := range []string{
		"delete ", "remove ", "drop ", "wipe", "rebuild",
		"reset ", "clear all", "scrap", "tear down",
	} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func enrichPrompt(text string) string {
	if destructiveIntent(text) {
		return "[INTENT: destructive] " +
			"Before making ANY destructive tool call (delete_entity, delete_page, delete_field, delete_hook, delete_route, delete_seed), " +
			"call propose_plan first describing what you intend to delete and why. " +
			"Wait for the user's next message confirming before executing destructive tool calls. " +
			"For non-destructive parts of the request, proceed normally.\n\n" +
			"User request: " + text
	}
	return text
}

// safeBuildArgs runs the adapter's argv builder under a recover guard:
// the watcher loop that drives turns has no per-request net, so a
// panicking adapter must cost one skipped turn, not the process.
func safeBuildArgs(logger *log.Logger, adapter Adapter, text string) (argv []string) {
	defer func() {
		if rec := recover(); rec != nil {
			// Scrubbed per the recoverlog rule: the panicked-on value
			// is adapter-supplied and must not forge log lines.
			logger.Printf("agent: adapter %q panicked building argv: %s", adapter.Name, textsafe.Recovered(rec))
			argv = nil
		}
	}()
	return adapter.BuildArgs(text)
}

func runOneAgentTurn(ctx context.Context, logger *log.Logger, tools *protocol.Tools, adapter Adapter, kilnURL, text string) {
	enriched := enrichPrompt(text)
	argv := safeBuildArgs(logger, adapter, enriched)
	if len(argv) == 0 {
		logger.Printf("agent: adapter %q produced empty argv", adapter.Name)
		return
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Env = append(os.Environ(), "KILN_URL="+kilnURL)
	c.Stderr = os.Stderr // surface diagnostic output to the kiln operator
	// The child runs in its own process group (childProcessGroup) and
	// Wait is bounded: an adapter that
	// backgrounds a pipe-holding descendant (sleep, tail -f, a server)
	// leaves the inherited stdout pipe open after the direct child
	// exits, and an unbounded Output drain would park this turn forever
	// — cancellation would have nothing left to kill, the superseded
	// note would never journal, and the goroutine leaks. WaitDelay is
	// the bound for exactly this shape. Cancel kills the whole group,
	// not just the direct child, and the group is killed again once the
	// turn ends: a shell the adapter backgrounded would otherwise keep
	// calling Kiln after a cancel, a supersede, or a normal exit.
	c.SysProcAttr = childProcessGroup()
	c.Cancel = func() error { return killGroup(c.Process) }
	c.WaitDelay = agentTurnWaitDelay
	stdout := &cappedBuffer{limit: maxAgentStdout}
	c.Stdout = stdout
	// Adapters that ask for a clean working directory (e.g. pi, which
	// will cat any Go file in cwd and report on it as if it were the
	// kiln world) get one. The registry name is a fixed path in the
	// shared temp root, so it is created owner-only and a name we
	// cannot tighten to 0700 (chmod requires ownership) is refused
	// rather than trusted — a pre-planted dir would otherwise hand the
	// agent someone else's files as its context. The actual cwd is a
	// unique per-turn MkdirTemp child, so two kiln users (or two turns)
	// never collide and planted files never sit in the agent's cwd.
	if adapter.Dir != "" {
		if err := os.Mkdir(adapter.Dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) { //gofastr:allow(fixedtmp) the fixed parent is the adapter registry's documented contract (a converted test pins /tmp/kiln-omp exists at 0700); it is created owner-only, a dir we cannot chmod 0700 (i.e. do not own) is refused below, and the agent's actual cwd is the per-turn MkdirTemp child
			logger.Printf("agent: adapter %q work dir %s: %v", adapter.Name, adapter.Dir, err)
			return
		}
		if err := claimAdapterDir(adapter.Dir); err != nil {
			logger.Printf("agent: adapter %q work dir %s: %v; refusing to run in it", adapter.Name, adapter.Dir, err)
			return
		}
		if err := fileperm.Restrict(adapter.Dir, true); err != nil {
			logger.Printf("restrict agent working directory: %v", err)
			return
		}
		turnDir, err := os.MkdirTemp(adapter.Dir, "turn-")
		if err != nil {
			logger.Printf("agent: adapter %q work dir %s: %v", adapter.Name, adapter.Dir, err)
			return
		}
		c.Dir = turnDir
		defer os.RemoveAll(turnDir)
	}
	err := c.Run()
	if c.Process != nil {
		_ = killGroup(c.Process)
	}
	resp := strings.TrimSpace(string(stdout.Bytes()))

	// Cancellation path: this turn was superseded. Render the right
	// reason: a newer message vs. a runtime agent switch. Use a fresh
	// context for the journal write; the original ctx is done.
	if ctx.Err() != nil {
		cause := context.Cause(ctx)
		var note string
		switch cause {
		case errAgentSwitched:
			note = "(superseded by agent harness switch: partial work above is preserved)"
			logger.Printf("agent: %v", cause)
		case errSupersededByNewMessage:
			note = "(superseded by newer message: partial work above is preserved)"
			logger.Printf("agent: %v", cause)
		case errCancelledByUser:
			note = "(cancelled by user: partial work above is preserved)"
			logger.Printf("agent: %v", cause)
		default:
			note = "(turn cancelled: partial work above is preserved)"
			logger.Printf("agent: cancelled (cause=%v)", cause)
		}
		_ = tools.Chat(context.Background(), protocol.ChatArgs{Role: "assistant", Text: note})
		return
	}

	if err != nil {
		logger.Printf("agent: %v", err)
		if resp == "" {
			resp = fmt.Sprintf("(agent error: %v)", err)
		}
	}
	if resp != "" {
		tools.Chat(ctx, protocol.ChatArgs{Role: "assistant", Text: resp})
	}
}
