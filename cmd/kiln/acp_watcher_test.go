//go:build unix

package main

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// TestWatcherSkipsACPMessages: a user message an ACP session journaled
// belongs to that session's turn. kiln acp answers it itself (refused
// with no provider), so the chat watcher running its own agent on it ran
// a prompt the client was told was refused.
func TestWatcherSkipsACPMessages(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	var mu sync.Mutex
	var prompts []string
	seen := func(want string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range prompts {
			if strings.Contains(p, want) {
				return true
			}
		}
		return false
	}
	store := NewAdapterStore(Adapter{Name: "record", BuildArgs: func(text string) []string {
		mu.Lock()
		prompts = append(prompts, text)
		mu.Unlock()
		return []string{"/bin/true"}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runAgentWatcher(ctx, log.New(io.Discard, "", 0), l, tools, store, ":1")

	chat := func(text, session string) {
		t.Helper()
		if res := tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: text, ACPSession: session}); !res.OK {
			t.Fatalf("chat %q: %s", text, res.Error)
		}
	}
	waitFor := func(want string, resend func()) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !seen(want) {
			if time.Now().After(deadline) {
				t.Fatalf("the watcher never ran %q", want)
			}
			if resend != nil {
				resend()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// The watcher subscribes on its own goroutine: send until it hears.
	waitFor("panel-warm", func() { chat("panel-warm", "") })

	chat("acp-refused", "acp-session-1")
	chat("panel-after", "")
	waitFor("panel-after", nil) // events arrive in order, so the ACP one was handled first
	if seen("acp-refused") {
		t.Fatal("the chat watcher ran an ACP session's prompt")
	}
}
