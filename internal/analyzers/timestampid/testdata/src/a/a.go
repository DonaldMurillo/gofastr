// Package a holds the timestampid fixture reduced from the real bug
// sites: kiln/acp/acp.go NewSession, messageID; kiln/chat/server.go
// applyEntry, nextCallID; kiln/protocol/protocol.go nextEntryID; and
// the crypto/rand-failure fallbacks battery/auth/apitoken.go
// generateAPITokenID, battery/auth/entity_store.go generateUserID,
// core/stream/sse_broker.go generateSubscriberID, core/stream
// websocket.go randomConnectionID, framework/audit.go writeAuditRow
// (probes TestAcpSessionRedUnpredictableID, 2026-09-03 round 5, no fix
// applied). The crypto/rand controls sit next to each.
package a

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"
)

// Agent mirrors kiln/acp.Agent, reduced to the mint.
type Agent struct {
	counter  atomic.Int64
	sessions map[string]bool
}

// NewSession, pre-fix (acp.go:91): the session id is a nanosecond
// clock plus a per-process counter, and session/load treats the bare
// id as the replay capability.
func (a *Agent) NewSession(cwd string) (bool, error) {
	id := fmt.Sprintf("kiln-%d-%d", time.Now().UnixNano(), a.counter.Add(1)) // want `id minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	a.sessions[id] = true
	return true, nil
}

// messageID, pre-fix (acp.go:158): no counter, still wall-clock.
func messageID(sess string) string {
	return fmt.Sprintf("msg_%s_%d", sess, time.Now().UnixNano()) // want `messageID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
}

// Server mirrors kiln/chat.Server, reduced to the two mints.
type Server struct {
	callCounter atomic.Int64
}

// nextCallID, pre-fix (server.go:464): the tool_call/tool_result
// pairing id.
func (s *Server) nextCallID() string {
	n := s.callCounter.Add(1)
	return fmt.Sprintf("c%d-%d", time.Now().UnixNano(), n) // want `nextCallID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
}

// applyEntry, pre-fix (server.go:472): the journal entry id, the
// `1788464074376701000-1` shape.
func (s *Server) applyEntry() error {
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), s.callCounter.Add(1)) // want `id minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	_ = id
	return nil
}

// nextEntryID, pre-fix (kiln/protocol/protocol.go:48). The doc
// comment there claims a per-process random suffix; the suffix is the
// counter above.
func (t *Server) nextEntryID() string {
	n := t.callCounter.Add(1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n) // want `nextEntryID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
}

// generateAPITokenID, reduced: the happy path draws 16 crypto/rand
// bytes (control), the crypto/rand-failure fallback mints a
// wall-clock token id.
func generateAPITokenID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tok-%d", time.Now().UnixNano()) // want `generateAPITokenID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	}
	return hex.EncodeToString(b)
}

// generateUserID, reduced (battery/auth/entity_store.go:616).
func generateUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("user-%d", time.Now().UnixNano()) // want `generateUserID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	}
	return fmt.Sprintf("%x", b)
}

// generateSubscriberID, reduced (core/stream/sse_broker.go:548): the
// strconv spelling.
func generateSubscriberID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16) // want `generateSubscriberID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	}
	return hex.EncodeToString(buf[:])
}

// randomConnectionID, reduced (core/stream/websocket.go:301).
func randomConnectionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%016x", time.Now().UnixNano()) // want `randomConnectionID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	}
	return fmt.Sprintf("%x", b[:])
}

// writeAuditRow, reduced (framework/audit.go:515): the concat
// spelling, and only 8 bytes of crypto/rand — below the 16-byte bar,
// so the wall-clock component still carries the id.
func writeAuditRow() {
	var rb [8]byte
	_, _ = rand.Read(rb[:])
	id := "aud_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + hex.EncodeToString(rb[:]) // want `id minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	_ = id
}

// throughLocal: the wall-clock reading sits in a local first.
func throughLocal() {
	n := time.Now().UnixNano()
	msgID := strconv.FormatInt(n, 36) // want `msgID minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	_ = msgID
}

// nowLocal: a local bound to time.Now() itself.
func nowLocal() {
	now := time.Now()
	nonce := fmt.Sprintf("n-%d", now.UnixNano()) // want `nonce minted from wall-clock time is enumerable; mint from crypto/rand \(ids package\)`
	_ = nonce
}

// rawNano is capability-NAMED but never formatted into a string: the
// int64 is a value, not a minted id.
func rawNano() int64 {
	entryID := time.Now().UnixNano()
	return entryID
}

// newSessionID is the harness ids / ULID control: the same expression
// carries 16 crypto/rand bytes, so the timestamp is ordering, not
// identity.
func newSessionID() string {
	var rb [16]byte
	_, _ = rand.Read(rb[:])
	return fmt.Sprintf("sess_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(rb[:]))
}

// createdAt is a timestamp value, not an identifier: unformatted
// int64, and the name never matches.
func createdAt() int64 {
	return time.Now().UnixMilli()
}

// stampField stores a time.Time: no string mint at all.
func stampField(e *entry) {
	e.expiresAt = time.Now()
}

type entry struct {
	expiresAt time.Time
}

// logFileName is a file name, not a capability: the name never
// matches.
func logFileName(app string) string {
	return fmt.Sprintf("%s-%d.log", app, time.Now().UnixNano())
}

// uniqueFilename mirrors core/upload UniqueFilename: an 8-byte random
// suffix defeats O_TRUNC collisions; the function name is not
// capability-shaped.
func uniqueFilename(name string) string {
	var b [8]byte
	rand.Read(b[:])
	return fmt.Sprintf("%s_%d_%s", name, time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// observability ids label builds, traces, and requests: developer
// labels, not bearer capabilities.
func observability() {
	buildID := strconv.FormatInt(time.Now().UnixNano(), 10)
	traceID := fmt.Sprintf("t-%d", time.Now().UnixNano())
	requestID := fmt.Sprintf("r-%d", time.Now().UnixNano())
	_, _, _ = buildID, traceID, requestID
}

// mintAt is the injectable-clock posture: a clock passed as a
// parameter (ulid.NewAt's shape) is not time.Now().
func mintAt(now time.Time) string {
	id := fmt.Sprintf("%d", now.UnixNano())
	return id
}
