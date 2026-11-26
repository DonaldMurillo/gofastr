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

var agentTurnWaitDelay = 2 * time.Second

var agentShutdownWait = agentTurnWaitDelay + 3*time.Second

const maxAgentStdout = 4 << 20

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
	return len(p), nil
}

func (b *cappedBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *cappedBuffer) Len() int      { return b.buf.Len() }

func countToolsAndErrors(l *live.Live) (calls, errors int) {
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

// runAgentWatcher runs the store's adapter once per chat_user event, a new
// message cancelling the turn before it, and returns once its turns have
// ended or agentShutdownWait has passed.
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
			if text == "" || acpSession != "" {
				continue
			}

			adapter := store.Get()
			if adapter.BuildArgs == nil {
				continue
			}

			turnCtx, cancel := context.WithCancelCause(ctx)
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

// adapterDirOpened is a test hook between claimAdapterDir's open and chmod.
var adapterDirOpened = func() {}

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

func chatTextByEntryID(l *live.Live, id string) (text, acpSession string) {
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

func safeBuildArgs(logger *log.Logger, adapter Adapter, text string) (argv []string) {
	defer func() {
		if rec := recover(); rec != nil {
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
	c.Stderr = os.Stderr
	c.SysProcAttr = childProcessGroup()
	c.Cancel = func() error { return killGroup(c.Process) }
	c.WaitDelay = agentTurnWaitDelay
	stdout := &cappedBuffer{limit: maxAgentStdout}
	c.Stdout = stdout
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
