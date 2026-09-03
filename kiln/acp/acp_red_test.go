//go:build red

package acp_test

// RED TEST — open finding, 2026-09-03 adversarial pass round 5 (tests-only;
// no fix applied).
//
// Property: capability-bearing identifiers carry >=128 unpredictable bits.
// Every credential this repo mints holds that bar from crypto/rand: setup
// tokens 256 bits, session tokens 256, api-token values 160, oauth state
// 128+HMAC. The ACP session ID is the one capability-shaped id with zero.
//
// Surfaces: kiln/acp/acp.go:91 — NewSession mints
// fmt.Sprintf("kiln-%d-%d", time.Now().UnixNano(), a.counter.Add(1)):
// the entropy is a nanosecond clock (guessable window) plus a per-process
// counter that starts at 1. LoadSession (acp.go:103-109) accepts the bare
// ID with no bearer binding and replays the journaled conversation — the
// ID IS the capability.
//
// Finding: a second client that can reach the kiln ACP surface can
// enumerate session IDs without ever minting one: the counter component is
// tiny and the UnixNano component narrows to a wall-clock window, so
// guessing a live session's ID (and replaying its chat via session/load)
// is practical. LoadSession does refuse never-minted IDs
// (ErrSessionNotFound, pinned by TestLoadUnknownSessionIsResourceNotFound)
// — but refusal of unknown IDs does not stop guessing minted ones.
//
// Severity: P3, labeled honestly — the package doc pins the world as
// process-local and loopback-dev-first, so exploitation needs the server
// exposed (port-forward, shared host, container with a published port).
// This is the unguessable-capability bar the rest of the repo already
// meets, pinned for the day the surface leaves loopback.
//
// Fix direction: mint the ID from crypto/rand — 16 random bytes, hex,
// matching generateAPITokenID/generateUserID's 16-byte shape — and drop
// the timestamp+counter structure (it buys nothing: sessions are keyed by
// ID in a map, not ordered).

import (
	"context"
	"regexp"
	"testing"

	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
)

// kilnMintShape is exactly what acp.go:91 produces today:
// kiln-<UnixNano digits>-<counter digits>.
var kilnMintShape = regexp.MustCompile(`^kiln-[0-9]+-[0-9]+$`)

// nanoRun matches any run of digits long enough to be a nanosecond
// timestamp embedded in an id, whatever prefix decorates it.
var nanoRun = regexp.MustCompile(`[0-9]{15,}`)

func TestAcpSessionRedUnpredictableID(t *testing.T) {
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
			t.Errorf("SECURITY: [predictable-session-id] ACP session id %q is the exact kiln-<UnixNano>-<counter> shape minted at acp.go:91 — zero unpredictable bits. session/load (acp.go:103) treats the bare ID as the replay capability with no bearer binding, so an enumerable ID is a bearer credential for the whole journaled conversation; every other capability this repo mints draws >=128 bits from crypto/rand. Mint the id from 16 crypto/rand bytes instead", id)
		}
		if run := nanoRun.FindString(id); run != "" {
			t.Errorf("SECURITY: [timestamp-derived-id] ACP session id %q embeds a %d-digit numeric run — nanosecond timestamps collapse the id space to a guessable wall-clock window regardless of what surrounds them; an unpredictable capability id must not carry one (mint from crypto/rand, acp.go:91)", id, len(run))
		}
	}
}
