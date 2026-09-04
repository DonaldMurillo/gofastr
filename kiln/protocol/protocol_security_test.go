package protocol

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
)

// nanoRun matches any run of digits long enough to be a nanosecond
// timestamp embedded in an id.
var nanoRun = regexp.MustCompile(`[0-9]{15,}`)

// Property: journal entry ids are minted from crypto/rand, never from a
// wall-clock timestamp or counter — the ids surface in panel state and
// ACP frames, and a timestamp-derived id is enumerable.
func TestNextEntryIDUnpredictable(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := New(l)
	for range 2 {
		id := tools.nextEntryID()
		if id == "" {
			t.Fatal("nextEntryID returned an empty id")
		}
		if run := nanoRun.FindString(id); run != "" {
			t.Errorf("entry id %q embeds a %d-digit numeric run — nanosecond timestamps collapse the id space to a guessable wall-clock window; mint from crypto/rand", id, len(run))
		}
	}
}
