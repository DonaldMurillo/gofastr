package journal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// ReadFile is the reader for a journal another process may be writing
// (kiln freeze beside kiln serve). A trailing fragment can be an Append
// in flight, so it is skipped and never truncated, and the file is opened
// read-only: a 0400 journal still reads.
func TestReadFileLeavesTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	j, err := journal.OpenJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"1", "2"} {
		e, err := journal.NewEntry(id, t0, journal.KindChatUser, "", journal.ChatMessagePayload{Text: "hi " + id})
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"3","kind":"chat_us`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}

	entries, err := journal.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(entries) != 2 || entries[1].ID != "2" {
		t.Fatalf("entries = %+v, want the two complete lines", entries)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("ReadFile changed the journal:\nbefore %q\nafter  %q", before, after)
	}
}
