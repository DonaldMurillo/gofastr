package journal

import (
	"errors"
	"os"
	"testing"
	"time"
)

// A short write (disk full) left its fragment in the file, and the next
// Append finished it into one newline-terminated line of invalid JSON:
// every later Read refused the journal and the next boot could not replay
// it (2026-10-27 K1). A failed Append now cuts its bytes back off.
func TestFailedAppendLeavesNoFragment(t *testing.T) {
	cases := []struct {
		name  string
		write func(*os.File, []byte) (int, error)
		sync  func(*os.File) error
	}{
		{"short write", func(f *os.File, b []byte) (int, error) {
			n, _ := f.Write(b[:len(b)/2])
			return n, errors.New("no space left on device")
		}, (*os.File).Sync},
		{"fsync", (*os.File).Write, func(*os.File) error {
			return errors.New("input/output error")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := openTestJSONL(t)
			appendText(t, j, "before")

			writeJournal, syncJournal = tc.write, tc.sync
			_, err := j.Append(textEntry(t, "lost"))
			writeJournal, syncJournal = (*os.File).Write, (*os.File).Sync
			if err == nil {
				t.Fatal("the failed append reported success")
			}

			appendText(t, j, "after")
			got, err := j.Read()
			if err != nil {
				t.Fatalf("read after a failed append: %v", err)
			}
			if len(got) != 2 || got[0].ID != "before" || got[1].ID != "after" {
				t.Fatalf("entries = %v, want before and after", entryIDs(got))
			}
			if n, _ := j.Len(); n != 2 {
				t.Fatalf("Len = %d, want 2", n)
			}
		})
	}
}

// When the cut fails too, the journal refuses further appends rather
// than splice the next line onto the fragment.
func TestUncutFragmentRefusesAppends(t *testing.T) {
	j := openTestJSONL(t)
	appendText(t, j, "before")

	writeJournal = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, errors.New("no space left on device")
	}
	truncateJournal = func(*os.File, int64) error { return errors.New("read-only file system") }
	_, err := j.Append(textEntry(t, "lost"))
	writeJournal, truncateJournal = (*os.File).Write, (*os.File).Truncate
	if err == nil {
		t.Fatal("the failed append reported success")
	}

	before, err := os.ReadFile(j.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(textEntry(t, "after")); err == nil {
		t.Fatal("an append onto an uncut fragment was accepted")
	}
	after, err := os.ReadFile(j.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("the refused append still wrote to the file")
	}
}

func textEntry(t *testing.T, id string) Entry {
	t.Helper()
	e, err := NewEntry(id, time.Unix(1, 0).UTC(), KindChatUser, "", ChatMessagePayload{Text: id})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func appendText(t *testing.T, j *JSONL, id string) {
	t.Helper()
	if _, err := j.Append(textEntry(t, id)); err != nil {
		t.Fatalf("append %s: %v", id, err)
	}
}

func entryIDs(es []Entry) []string {
	ids := make([]string, len(es))
	for i, e := range es {
		ids[i] = e.ID
	}
	return ids
}
