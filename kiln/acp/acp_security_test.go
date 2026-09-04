package acp_test

import (
	"context"
	"regexp"
	"testing"

	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
)

// kilnMintShape is the timestamp+counter shape the session id must never
// have again: kiln-<UnixNano digits>-<counter digits>.
var kilnMintShape = regexp.MustCompile(`^kiln-[0-9]+-[0-9]+$`)

// randShape is the shape kid.Hex(16) mints: 32 lowercase hex characters,
// 128 bits of crypto/rand. A positive shape check is the honest pin: a
// "no long digit run" heuristic fails on random hex itself about one
// time in a hundred, since ten of the sixteen hex characters are digits.
var randShape = regexp.MustCompile(`^kiln-[0-9a-f]{32}$`)

// allDigits is a zero-padded decimal timestamp wearing the hex shape;
// 16 random bytes are all digits with probability (10/16)^32, never.
var allDigits = regexp.MustCompile(`^kiln-[0-9]{32}$`)

// Property: the ACP session id is a capability (session/load replays the
// whole journaled conversation on the bare id), so it is minted from
// crypto/rand — never from a wall-clock timestamp or a counter, which
// collapse the id space to a guessable window.
func TestAcpSessionIDUnpredictable(t *testing.T) {
	tools := newTools(t)
	a := kilnacp.New(tools)

	var ids []string
	for range 2 {
		s, err := a.NewSession(context.Background(), "")
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		ids = append(ids, s.ID())
	}
	if ids[0] == ids[1] {
		t.Fatalf("minted two sessions with the same id %q — test premise broken", ids[0])
	}

	for _, id := range ids {
		if kilnMintShape.MatchString(id) {
			t.Errorf("ACP session id %q is the exact kiln-<UnixNano>-<counter> shape — zero unpredictable bits; session/load treats the bare id as the replay capability, so it must be minted from crypto/rand", id)
		}
		if allDigits.MatchString(id) {
			t.Errorf("ACP session id %q is all digits: a padded timestamp, not 16 bytes of crypto/rand", id)
		}
		if !randShape.MatchString(id) {
			t.Errorf("ACP session id %q is not kiln-<32 hex> (16 bytes of crypto/rand): a timestamp or counter cannot produce that shape, anything else is a weaker mint", id)
		}
	}
}
