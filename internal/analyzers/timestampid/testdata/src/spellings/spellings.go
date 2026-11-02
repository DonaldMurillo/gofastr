// Package spellings holds the wall-clock id spellings the 2026-11-02
// guard bash found timestampid blind to: the id bound in a composite
// literal's keyed field or a var declaration, fmt.Sprint, and the time
// formatted as text (Format, String) rather than read as Unix*.
package spellings

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Session struct {
	ID    string
	Owner string
}

func newSession(owner string) *Session {
	return &Session{ID: fmt.Sprintf("s-%d", time.Now().UnixNano()), Owner: owner} // want `ID minted from wall-clock time is enumerable`
}

func varDecl() string {
	var sessionToken = fmt.Sprintf("t-%d", time.Now().UnixNano()) // want `sessionToken minted from wall-clock time is enumerable`
	return sessionToken
}

func sprint() string {
	nonce := fmt.Sprint(time.Now().UnixNano()) // want `nonce minted from wall-clock time is enumerable`
	return nonce
}

func formatted() string {
	apiKey := "k-" + time.Now().Format("20060102150405.000000000") // want `apiKey minted from wall-clock time is enumerable`
	return apiKey
}

func newCapabilityID() string {
	return time.Now().Format(time.RFC3339Nano) // want `newCapabilityID minted from wall-clock time is enumerable`
}

// Silent: 16 bytes of crypto/rand from a make'd buffer ride along; a
// value, not an id; and a timestamp field.
func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

func shortRandID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("s_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b)) // want `shortRandID minted from wall-clock time is enumerable`
}

type Event struct {
	At    string
	Label string
}

func stamp() Event {
	return Event{At: time.Now().Format(time.RFC3339), Label: "x"}
}

// Silent: a date-bucketed storage or cache key names a place, not a
// capability. Guessing "backups/20261102-0300.sql" grants nothing the
// store's own access control did not already decide.
type Object struct {
	Key  string
	Size int64
}

func backupObject() Object {
	return Object{Key: "backups/" + time.Now().Format("20060102-150405") + ".sql"}
}

func statsCacheKey() string {
	cacheKey := "stats:" + time.Now().Format("2006-01-02")
	return cacheKey
}

func bucketKey() string {
	objectKey := fmt.Sprintf("logs/%d.json", time.Now().Unix())
	return objectKey
}

// A key qualified as a credential still fires.
func newSigningKey() string {
	signingKey := fmt.Sprintf("sk-%d", time.Now().UnixNano()) // want "signingKey minted from wall-clock time is enumerable"
	return signingKey
}
