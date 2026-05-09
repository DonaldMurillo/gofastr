package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

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
func runAgentWatcher(ctx context.Context, logger *log.Logger, l *live.Live, tools *protocol.Tools, cmd, addr string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	kilnURL := "http://" + host

	ch, unsub := l.Subscribe()
	defer unsub()

	var (
		mu       sync.Mutex
		curCtx   context.Context
		curCancl context.CancelFunc
	)
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			if curCancl != nil {
				curCancl()
			}
			mu.Unlock()
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

			mu.Lock()
			if curCancl != nil {
				// Supersede the in-flight turn. The goroutine running
				// runOneAgentTurn will see ctx.Err() != nil after Output
				// returns and journal a (superseded) note.
				curCancl()
			}
			turnCtx, cancel := context.WithCancel(ctx)
			curCtx = turnCtx
			curCancl = cancel
			mu.Unlock()

			go func(turnCtx context.Context, cancel context.CancelFunc, text string) {
				defer cancel()
				runOneAgentTurn(turnCtx, logger, tools, parts, kilnURL, text)
				// Clear curCancl if it still points at this turn.
				mu.Lock()
				if curCtx == turnCtx {
					curCtx, curCancl = nil, nil
				}
				mu.Unlock()
			}(turnCtx, cancel, text)
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

func runOneAgentTurn(ctx context.Context, logger *log.Logger, tools *protocol.Tools, parts []string, kilnURL, text string) {
	enriched := enrichPrompt(text)
	args := append([]string(nil), parts[1:]...)
	args = append(args, enriched)
	c := exec.CommandContext(ctx, parts[0], args...)
	c.Env = append(os.Environ(), "KILN_URL="+kilnURL)
	c.Stderr = os.Stderr // surface diagnostic output to the kiln operator
	out, err := c.Output()
	resp := strings.TrimSpace(string(out))

	// Cancellation path: a newer chat_user superseded this turn. Use a
	// fresh context for the journal write — the original ctx is done.
	// Skip "agent error" journaling because the kill is intentional.
	if ctx.Err() != nil {
		logger.Printf("agent: superseded by newer message")
		// Use Background so the synthetic note still lands after cancel.
		_ = tools.Chat(context.Background(), protocol.ChatArgs{
			Role: "assistant",
			Text: "(superseded by newer message — partial work above is preserved)",
		})
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
