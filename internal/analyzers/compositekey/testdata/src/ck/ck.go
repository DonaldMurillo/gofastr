// Package ck holds the compositekey fixtures. The positives are reduced
// from the pre-fix code the probes broke: core/a2a/store_memory.go and
// core/middleware/idempotency.go at 7bd789e9 (fixed in b79942f7), with
// the real API names kept. The fixed spellings (struct keys, a
// length-prefixed shard) are the negatives beside them.
package ck

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
)

type TaskRecord struct {
	Owner string
	Title string
}

type PushConfigRecord struct {
	Owner  string
	TaskID string
	ID     string
}

// MemoryStore is the pre-fix a2a memory store: two maps keyed by
// delimiter-joined owner-scoped strings.
type MemoryStore struct {
	mu    sync.Mutex
	tasks map[string]*TaskRecord
	push  map[string]*PushConfigRecord
}

func taskKey(owner, id string) string { return owner + "\x00" + id } // want `taskKey joins parts with the "\\x00" separator into a key.*struct key`

func pushKey(owner, taskID, id string) string { // want `pushKey joins parts with the "\\x00" separator into a key`
	return owner + "\x00" + taskID + "\x00" + id
}

func (m *MemoryStore) CreateTask(_ context.Context, rec *TaskRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := taskKey(rec.Owner, rec.Title)
	if _, exists := m.tasks[key]; exists {
		return fmt.Errorf("conflict")
	}
	m.tasks[key] = rec
	return nil
}

func (m *MemoryStore) GetTask(_ context.Context, owner, id string) (*TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.tasks[taskKey(owner, id)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return rec, nil
}

func (m *MemoryStore) GetPush(_ context.Context, owner, taskID, id string) *PushConfigRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.push[pushKey(owner, taskID, id)] // the pushKey report lands on the helper itself
}

// ListTasks is the leak surface TestStoreOwnerKeyDelimiterNoLeak drove:
// the owner prefix scan matches any key whose owner part embeds the
// separator, so owner "alice\x00t1" lists alice's tasks.
func (m *MemoryStore) ListTasks(_ context.Context, owner string) []*TaskRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := owner + "\x00" // want `prefix joins parts with the "\\x00" separator into a key`
	var out []*TaskRecord
	for key, rec := range m.tasks {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// Touch joins inline at the sink: same shape without any intermediate.
func (m *MemoryStore) Touch(owner, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.tasks[owner+"\x00"+id] // want `this concatenation joins parts with the "\\x00" separator into a key`
	return ok
}

// LookupTable is the composite-literal spelling: a join in key position
// of a map literal.
func LookupTable(owner, id string) map[string]int {
	return map[string]int{owner + "\x00" + id: 1} // want `this concatenation joins parts with the "\\x00" separator into a key`
}

// IdempotencyStore is the Store.Begin shape from
// core/middleware/idempotency.go: the join feeds a keyed store behind an
// interface, not a map in this file.
type IdempotencyStore interface {
	Begin(ctx context.Context, key string, fingerprint string) (bool, error)
}

type idempotencyMiddleware struct {
	Store IdempotencyStore
}

func (mw *idempotencyMiddleware) claim(ctx context.Context, principal, key string) error {
	fp := "fingerprint"
	storeKey := principal + "\x00" + key // want `storeKey joins parts with the "\\x00" separator into a key`
	_, err := mw.Store.Begin(ctx, storeKey, fp)
	return err
}

// ---- fixed spellings (b79942f7), kept as the negative cases ---------

type taskStructKey struct{ owner, id string }

type FixedStore struct {
	mu    sync.Mutex
	tasks map[taskStructKey]*TaskRecord
}

func (m *FixedStore) CreateTask(rec *TaskRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := taskStructKey{rec.Owner, rec.Title} // struct key: injective by construction
	m.tasks[key] = rec
}

func (m *FixedStore) GetTask(owner, id string) *TaskRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[taskStructKey{owner, id}]
}

// claimFixed is the length-prefixed shard the fix shipped: the leading
// len(principal) pins the boundary, so the join is injective.
func (mw *idempotencyMiddleware) claimFixed(ctx context.Context, principal, key string) error {
	storeKey := strconv.Itoa(len(principal)) + "\x00" + principal + key
	_, err := mw.Store.Begin(ctx, storeKey, "fingerprint")
	return err
}

// ---- deliberate silences ---------------------------------------------

// PathJoin is a path join with "/": a different shape from an identity
// key, and not this rule's.
func PathJoin(a, b string, m map[string]string) string {
	k := a + "/" + b
	return m[k]
}

// MessageOnly joins with the separator but never indexes on it: printing
// an ambiguous string is not a collision.
func MessageOnly(owner, id string) string {
	msg := owner + "\x00" + id
	log.Println(msg)
	return msg
}

// ColonKey is the readable-domain shape (":", ".", " ", "|", "-"):
// printable separators are silent by design.
func ColonKey(owner, host string, m map[string]string) bool {
	_, ok := m[owner+":"+host]
	return ok
}

// SprintfKey builds its key with fmt.Sprintf: interpolation is the
// emitident/GOFASTR1401 family, not this one.
func SprintfKey(owner, id string, m map[string]int) bool {
	_, ok := m[fmt.Sprintf("%s\x00%s", owner, id)]
	return ok
}

// LogOnlyHelper is a join helper nobody keys on: collecting it is free,
// reporting it would be noise.
func LogOnlyHelper(a, b string) string {
	return a + "\x00" + b
}

// ClaimStore pins the keyed-store leg's declared name gate: the join
// feeds a keyed store behind an interface whose parameter is named
// claim, not "key" — this leg is name-gated (see the package doc), so
// it stays silent; the join itself is visible to no map sink here.
type ClaimStore interface {
	Put(ctx context.Context, claim string, v []byte) error
}

func putJoin(cs ClaimStore, ctx context.Context, owner, id string) error {
	return cs.Put(ctx, owner+"\x00"+id, nil)
}
