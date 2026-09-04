package protocol

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
)

// randShape is the shape kid.Hex(16) mints: 32 lowercase hex characters,
// 128 bits of crypto/rand. A positive shape check is the honest pin: a
// "no long digit run" heuristic fails on random hex itself about one
// time in a hundred, since ten of the sixteen hex characters are digits.
var randShape = regexp.MustCompile(`^e[0-9a-f]{32}$`)

// allDigits is a zero-padded decimal timestamp wearing the hex shape;
// 16 random bytes are all digits with probability (10/16)^32, never.
var allDigits = regexp.MustCompile(`^e[0-9]{32}$`)

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
		if allDigits.MatchString(id) {
			t.Errorf("entry id %q is all digits: a padded timestamp, not 16 bytes of crypto/rand", id)
		}
		if !randShape.MatchString(id) {
			t.Errorf("entry id %q is not e<32 hex> (16 bytes of crypto/rand): a timestamp or counter cannot produce that shape, anything else is a weaker mint", id)
		}
	}
}
