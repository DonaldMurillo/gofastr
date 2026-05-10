package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
)

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
// stays — Kiln's world is append-only, so partial work persists.
func runAgentWatcher(ctx context.Context, logger *log.Logger, l *live.Live, tools *protocol.Tools, store *AdapterStore, addr string) {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	kilnURL := "http://" + host

	ch, unsub := l.Subscribe()
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			// Process exit: cancel any in-flight turn through the store.
			store.Set(store.Get())
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Kind != string(journal.KindChatUser) {
				continue
			}
			text := chatTextByEntryID(l, ev.EntryID)
			if text == "" {
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
			store.SetTurnCancel(cancel)

			go func(turnCtx context.Context, cancel context.CancelCauseFunc, text string, adapter Adapter) {
				defer cancel(nil)
				l.Notify("agent_turn_started", adapter.Name)
				runOneAgentTurn(turnCtx, logger, tools, adapter, kilnURL, text)
				store.ClearTurnCancel()
				l.Notify("agent_turn_ended", adapter.Name)
			}(turnCtx, cancel, text, adapter)
		}
	}
}

func chatTextByEntryID(l *live.Live, id string) string {
	sess := l.Session()
	for _, e := range sess.Chat {
		if e.EntryID == id && e.Message != nil {
			return e.Message.Text
		}
	}
	return ""
}

// destructiveIntent runs a quick keyword scan on the user message
// before pi sees it. If the user asked for something destructive
// (delete, drop, wipe, rebuild, reset) we prepend a directive forcing
// the agent through propose_plan + user approval first. This is a
// pre-flight classifier — cheap, deterministic, no extra LLM call.
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

func runOneAgentTurn(ctx context.Context, logger *log.Logger, tools *protocol.Tools, adapter Adapter, kilnURL, text string) {
	enriched := enrichPrompt(text)
	argv := adapter.BuildArgs(enriched)
	if len(argv) == 0 {
		logger.Printf("agent: adapter %q produced empty argv", adapter.Name)
		return
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Env = append(os.Environ(), "KILN_URL="+kilnURL)
	c.Stderr = os.Stderr // surface diagnostic output to the kiln operator
	out, err := c.Output()
	resp := strings.TrimSpace(string(out))

	// Cancellation path: this turn was superseded. Render the right
	// reason: a newer message vs. a runtime agent switch. Use a fresh
	// context for the journal write — the original ctx is done.
	if ctx.Err() != nil {
		cause := context.Cause(ctx)
		var note string
		switch cause {
		case errAgentSwitched:
			note = "(superseded by agent harness switch — partial work above is preserved)"
			logger.Printf("agent: %v", cause)
		case errSupersededByNewMessage:
			note = "(superseded by newer message — partial work above is preserved)"
			logger.Printf("agent: %v", cause)
		default:
			note = "(turn cancelled — partial work above is preserved)"
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
