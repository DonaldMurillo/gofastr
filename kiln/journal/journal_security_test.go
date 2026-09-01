package journal

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Property: the journal write side must refuse any entry its own readers
// cannot scan back.
//
// Every reader of the JSONL journal caps its bufio.Scanner at 16 MiB:
// countLines (journal.go:126-140, run by OpenJSONL at boot), Read
// (journal.go:161-186, replay and freeze), and TruncateAfter
// (journal.go:188+, undo and reset_session). Append (journal.go:142-159)
// marshals and writes with no size bound, and nothing between the
// unauthenticated kiln tool dispatcher's body read and the journal
// bounds the payload, so one oversized entry (giant chat text, seed
// row, or page tree — the tool_call envelope journals the raw args a
// second time) writes a line the same process can never scan again:
// the next boot dies in countLines, Replay fails in Read, and
// Undo/ResetSession fail inside TruncateAfter, so recovery requires
// hand-editing the journal file. The framework caps its own mutating
// surface the same way (decodeBounded, framework/uihost/uihost.go); the
// journal write side is the outlier.

// scannerCap mirrors the readers' bufio.Scanner max token size
// (journal.go countLines/Read/TruncateAfter: 16*1024*1024).
const scannerCap = 16 * 1024 * 1024

func openTestJSONL(t *testing.T) *JSONL {
	t.Helper()
	j, err := OpenJSONL(filepath.Join(t.TempDir(), "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func TestAppendRejectsUnscannableLines(t *testing.T) {
	t.Run("oversized entry is refused", func(t *testing.T) {
		j := openTestJSONL(t)
		e, err := NewEntry("big", time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{
			Text: strings.Repeat("a", scannerCap+64),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := j.Append(e); err == nil {
			t.Errorf("SECURITY: Append accepted an entry whose marshaled line exceeds the 16 MiB scanner cap every reader enforces.\n"+
				"Attack: one oversized journaled payload (chat text, seed row, or page tree via the unauthenticated tool API) "+
				"bricks the journal durably — OpenJSONL dies at boot in countLines, Replay fails in Read, and Undo/ResetSession "+
				"fail inside TruncateAfter, all with %q. The write side must refuse what the read side cannot scan.",
				"token too long")
		}
		// A refused entry must leave the journal untouched.
		if entries, err := j.Read(); err != nil {
			t.Errorf("journal unreadable after a refused Append: %v", err)
		} else if len(entries) != 0 {
			t.Errorf("journal contains %d entr(ies) after a refused Append, want 0", len(entries))
		}
	})
	t.Run("in-cap entry stays readable", func(t *testing.T) {
		j := openTestJSONL(t)
		e, err := NewEntry("small", time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{
			Text: strings.Repeat("a", 1<<20),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := j.Append(e); err != nil {
			t.Fatalf("in-cap Append: %v", err)
		}
		entries, err := j.Read()
		if err != nil {
			t.Fatalf("Read after in-cap Append: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("Read returned %d entries, want 1", len(entries))
		}
		var p ChatMessagePayload
		if err := entries[0].Decode(&p); err != nil {
			t.Fatalf("decode round-trip: %v", err)
		}
		if len(p.Text) != 1<<20 {
			t.Errorf("text round-trip: got %d bytes, want %d", len(p.Text), 1<<20)
		}
	})
}
