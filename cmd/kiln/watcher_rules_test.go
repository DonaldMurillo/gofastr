//go:build unix

package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

func startWatcherForTest(t *testing.T, store *AdapterStore) (*live.Live, *protocol.Tools) {
	t.Helper()
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runAgentWatcher(ctx, log.New(io.Discard, "", 0), l, tools, store, ":1")
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return l, tools
}

func appendingAdapter(name, logFile string) Adapter {
	return Adapter{Name: name, BuildArgs: func(string) []string {
		return []string{"/bin/sh", "-c", "echo " + name + " >> \"" + logFile + "\""}
	}}
}

// sendUntil sends a user message every 50ms until done reports true.
func sendUntil(t *testing.T, tools *protocol.Tools, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		_ = tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "build"})
		time.Sleep(50 * time.Millisecond)
	}
}

func ranAdapters(logFile string) string {
	raw, _ := os.ReadFile(logFile)
	return string(raw)
}

func TestWatcherJournalsATurnSummary(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "ran.log")
	l, tools := startWatcherForTest(t, NewAdapterStore(appendingAdapter("a", logFile)))
	summarised := func() bool {
		found := false
		l.ReadSession(func(s *journal.Session) {
			for _, e := range s.Chat {
				if e.Kind == journal.KindChatAssistant && e.Message != nil && strings.HasPrefix(e.Message.Text, "[· no tools used · ") {
					found = true
				}
			}
		})
		return found
	}
	sendUntil(t, tools, "a journaled turn summary", summarised)
}

func TestWatcherReadsTheAdapterPerMessage(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "ran.log")
	store := NewAdapterStore(appendingAdapter("a", logFile))
	_, tools := startWatcherForTest(t, store)
	sendUntil(t, tools, "the first adapter to run", func() bool { return strings.Contains(ranAdapters(logFile), "a") })
	store.Set(appendingAdapter("b", logFile))
	sendUntil(t, tools, "the switched adapter to run", func() bool { return strings.Contains(ranAdapters(logFile), "b") })
}
