package journal

import (
	"errors"
	"os"
	"testing"
)

func TestMemoryTruncateReleasesRemovedEntries(t *testing.T) {
	j := NewMemory()
	if _, err := j.Append(textEntry(t, "gone")); err != nil {
		t.Fatal(err)
	}
	if err := j.TruncateAfter(0); err != nil {
		t.Fatal(err)
	}
	if got := j.entries[:1][0].Payload; got != nil {
		t.Fatalf("removed entry still retains payload: %s", got)
	}
}

func TestFailedTruncateLeavesJournalUsable(t *testing.T) {
	j := openTestJSONL(t)
	appendText(t, j, "original")
	renameJournal = func(string, string) error { return errors.New("rename failed") }
	err := j.TruncateAfter(0)
	renameJournal = os.Rename
	if err == nil {
		t.Fatal("expected rename error")
	}
	got, err := j.Read()
	if err != nil || len(got) != 1 {
		t.Fatalf("read after failed truncate: %v, %v", got, err)
	}
}
