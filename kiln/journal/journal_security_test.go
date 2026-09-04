package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
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

// Property: a crash-torn journal tail must cost exactly the in-flight
// entry — never the journal.
//
// JSONL's own contract (journal.go): "Each Append writes one line and
// fsyncs so a crash loses at most the in-flight entry." A final line
// with no trailing newline is the on-disk artifact of exactly that
// crash: the write was interrupted mid-line. Every reader instead
// treats it as a fatal parse error: countLines counts it, so
// OpenJSONL succeeds with a count that includes garbage, then Read
// dies on json.Unmarshal, Replay fails, live.New refuses to boot, and
// Undo/ResetSession fail inside TruncateAfter — the operator loses the
// whole session, not the in-flight entry, and recovery means
// hand-editing the file. Since Append always terminates a complete
// entry with '\n', a final line lacking one is unambiguously torn and
// safe to drop.
func TestTornTailLosesOnlyInFlightEntry(t *testing.T) {
	for name, torn := range map[string]string{
		"truncated JSON, no newline": `{"id":"3","ts":"2026-01-01T00:00:00Z","kind":"cha`,
		"lone open brace":            `{`,
		"whitespace-only tail":       "   ",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			j, err := OpenJSONL(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				e, err := NewEntry(fmt.Sprintf("ok-%d", i), time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{Text: "intact"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := j.Append(e); err != nil {
					t.Fatal(err)
				}
			}
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
			// Simulate the crash: the interrupted Append leaves partial
			// bytes with NO trailing newline.
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(torn); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			// Surface 1: boot — OpenJSONL must not choke on the torn tail.
			j2, err := OpenJSONL(path)
			if err != nil {
				t.Fatalf("SECURITY: reopen after a torn final write failed: %v\n"+
					"Attack: a crash mid-Append leaves a partial line; the journal's own contract says a crash loses\n"+
					"\"at most the in-flight entry\", but boot now dies in countLines and the whole session is lost.", err)
			}
			t.Cleanup(func() { _ = j2.Close() })

			// Surface 2: Read must return the intact prefix, not error.
			entries, err := j2.Read()
			if err != nil {
				t.Fatalf("SECURITY: Read after a torn final write failed: %v\n"+
					"The torn tail is the in-flight entry the contract already writes off; the two intact lines\n"+
					"before it must still replay (live.New boots, kiln freeze works).", err)
			}
			if len(entries) != 2 {
				t.Errorf("torn tail recovered to %d entries, want the 2 intact ones", len(entries))
			}

			// Surface 3: Len must agree with Read (Undo truncates by it).
			if n, err := j2.Len(); err != nil || n != len(entries) {
				t.Errorf("Len() = %d (err %v), want %d to agree with Read", n, err, len(entries))
			}

			// Surface 4: recovery must be durable — after truncation the
			// journal accepts and replays new entries (the Undo/ResetSession
			// path).
			if err := j2.TruncateAfter(0); err != nil {
				t.Fatalf("truncate after recovery: %v", err)
			}
			e3, err := NewEntry("after-crash", time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{Text: "recovered"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := j2.Append(e3); err != nil {
				t.Fatalf("append after recovery: %v", err)
			}
			got, err := j2.Read()
			if err != nil {
				t.Fatalf("read after recovery: %v", err)
			}
			if len(got) != 1 || got[0].ID != "after-crash" {
				t.Errorf("journal after recovery = %+v, want exactly the post-crash entry", got)
			}
		})
	}
}

// Compaction bounds: both implementations must refuse a truncate offset
// below zero or past the end rather than silently corrupting state.
// Undo feeds this TruncateAfter(n-1) and ResetSession feeds 0, so an
// off-by-one acceptance here would truncate the wrong prefix.
func TestTruncateBoundsRefused(t *testing.T) {
	mem := NewMemory()
	e, err := NewEntry("1", time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Append(e); err != nil {
		t.Fatal(err)
	}
	for _, impl := range []struct {
		name string
		j    Journal
	}{
		{"memory", mem},
		{"jsonl", openTestJSONL(t)},
	} {
		if err := impl.j.TruncateAfter(-1); err == nil {
			t.Errorf("%s: TruncateAfter(-1) accepted", impl.name)
		}
		if err := impl.j.TruncateAfter(99); err == nil {
			t.Errorf("%s: TruncateAfter past the end accepted", impl.name)
		}
	}
}

// Compaction pin: TruncateAfter must preserve the surviving prefix
// byte-for-byte (same IDs, same order) and leave the journal writable
// across a reopen — Undo works by truncating the last entry, so any
// rewriting of the prefix would corrupt history the log still claims.
func TestTruncatePreservesPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	j, err := OpenJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := range 3 {
		e, err := NewEntry(fmt.Sprintf("keep-%d", i), time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{Text: "m"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
		if _, err := j.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.TruncateAfter(2); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	j2, err := OpenJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	entries, err := j2.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != ids[0] || entries[1].ID != ids[1] {
		t.Errorf("prefix after truncate+reopen = %+v, want [%s %s] in order", entries, ids[0], ids[1])
	}
	e, err := NewEntry("next", time.Now().UTC(), KindChatUser, Op(""), ChatMessagePayload{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	off, err := j2.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	if off != 3 {
		t.Errorf("Append after truncate returned offset %d, want 3 (count carried across compaction + reopen)", off)
	}
}

// Property: the journal file is owner-only (0600 in a 0700 dir). Its
// lines embed app-config verbatim — Auth.JWTSecret and
// Admin.SeedPassword, the same data freeze writes owner-only — so the
// file must not be group/world-readable wherever it lands.
func TestJournalRestrictsSecretFile(t *testing.T) {
	const jwtSecret = "pin-jwt-secret-0123456789abcdef"
	const seedPassword = "pin-seed-password-ghijkl" // not-a-secret: test fixture value asserted to land in the journal

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	j, err := OpenJSONL(path)
	if err != nil {
		t.Fatalf("OpenJSONL: %v", err)
	}
	e, err := NewEntry("cfg-pin", time.Now().UTC(), KindWorldEdit, OpSetAppConfig, SetAppConfigPayload{
		Config: world.AppConfig{
			Name:  "pinapp",
			Auth:  world.AuthConfig{Enabled: true, JWTSecret: jwtSecret},
			Admin: world.AdminConfig{Enabled: true, SeedEmail: "ops@pin.test", SeedPassword: seedPassword},
		},
	})
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if _, err := j.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Ground the severity: the credential values land in the file
	// verbatim. If this ever fails because the journal substitutes env
	// refs (as freeze does), this pin must be revisited, not kept.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(data), jwtSecret) || !strings.Contains(string(data), seedPassword) {
		t.Fatalf("journal no longer stores app-config secrets verbatim; revisit this pin (payload=%s)", data)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("journal holding Auth.JWTSecret / Admin.SeedPassword is mode %o — group/world readable; "+
			"the journal is the replay source for the exact data freeze writes owner-only, so it must be 0600",
			fi.Mode().Perm())
	}
}
