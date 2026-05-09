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
// Spawn semantics: serialized — at most one agent runs at a time. If a
// second chat_user arrives while the first is still running, it queues
// behind. This matches the user's mental model of a turn-by-turn chat.
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

	var serial sync.Mutex
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
			// Find the matching chat event in the session and pull its text.
			text := chatTextByEntryID(l, ev.EntryID)
			if text == "" {
				continue
			}
			go func(text string) {
				serial.Lock()
				defer serial.Unlock()
				runOneAgentTurn(ctx, logger, tools, parts, kilnURL, text)
			}(text)
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
