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

// nanoRun matches any run of digits long enough to be a nanosecond
// timestamp embedded in an id, whatever prefix decorates it.
var nanoRun = regexp.MustCompile(`[0-9]{15,}`)

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
		if run := nanoRun.FindString(id); run != "" {
			t.Errorf("ACP session id %q embeds a %d-digit numeric run — nanosecond timestamps collapse the id space to a guessable wall-clock window regardless of what surrounds them", id, len(run))
		}
	}
}
