package a2a

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// MemoryStore is the in-process Store: a mutex, two maps, deep copies
// at the boundary. Every read and write is scoped by owner in the map
// key, not filtered after the fact, so the owner predicate cannot be
// forgotten on a new query shape. Use the SQLStore to share tasks
// across replicas.
type MemoryStore struct {
	mu    sync.Mutex
	tasks map[taskKey]*TaskRecord
	push  map[pushKey]*PushConfigRecord
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks: map[taskKey]*TaskRecord{},
		push:  map[pushKey]*PushConfigRecord{},
	}
}

// taskKey and pushKey are the store's map keys. They are structs, not
// owner+"\x00"+id string concatenations: a string composite is
// ambiguous when a field itself contains NUL — ("alice\x00t1", "evil")
// collides with ("alice", "t1\x00evil") on GetTask, and ListTasks'
// prefix scan matched the former against alice's "owner\x00" prefix,
// leaking one owner's task list to another. Struct keys are injective
// by construction; no delimiter can be smuggled through them.
type taskKey struct{ owner, id string }

type pushKey struct{ owner, taskID, id string }

func (m *MemoryStore) CreateTask(_ context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := taskKey{rec.Owner, rec.Task.ID}
	if _, exists := m.tasks[key]; exists {
		return ErrConflict
	}
	stored := rec.Clone()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now()
	}
	stored.UpdatedAt = stored.CreatedAt
	m.tasks[key] = stored
	rec.CreatedAt, rec.UpdatedAt = stored.CreatedAt, stored.UpdatedAt
	return nil
}

func (m *MemoryStore) GetTask(_ context.Context, owner, id string) (*TaskRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.tasks[taskKey{owner, id}]
	if !ok {
		// Unknown id and someone else's task answer identically: task
		// ids must not enumerate across owners.
		return nil, ErrNotFound
	}
	return rec.Clone(), nil
}

func (m *MemoryStore) UpdateTask(_ context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := taskKey{rec.Owner, rec.Task.ID}
	stored, ok := m.tasks[key]
	if !ok {
		return ErrNotFound
	}
	if stored.Version != rec.Version {
		return ErrConflict
	}
	next := rec.Clone()
	next.Version = stored.Version + 1
	next.CreatedAt = stored.CreatedAt
	next.UpdatedAt = time.Now()
	m.tasks[key] = next
	*rec = *next
	return nil
}

func (m *MemoryStore) ListTasks(_ context.Context, owner string, q ListQuery) (recs []*TaskRecord, total int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []*TaskRecord
	for key, rec := range m.tasks {
		// Exact owner equality, not a delimiter prefix: with string
		// keys, an owner literally named "alice\x00t1" made alice's
		// prefix scan match its rows. The struct key makes the fields
		// unambiguous; compare the field, never a concatenation.
		if key.owner != owner {
			continue
		}
		if !matchesQuery(rec, q) {
			continue
		}
		matched = append(matched, rec)
	}
	sortRecsNewestFirst(matched)
	total = len(matched)
	if q.Offset > 0 {
		if q.Offset >= total {
			return nil, total, nil
		}
		matched = matched[q.Offset:]
	}
	if q.Limit > 0 && q.Limit < len(matched) {
		matched = matched[:q.Limit]
	}
	for _, rec := range matched {
		recs = append(recs, rec.Clone())
	}
	return recs, total, nil
}

// matchesQuery applies ListQuery to one record. Status timestamps sort
// with nil treated as the zero instant.
func matchesQuery(rec *TaskRecord, q ListQuery) bool {
	if q.ContextID != "" && rec.Task.ContextID != q.ContextID {
		return false
	}
	if q.Status != "" && rec.Task.Status.State != q.Status {
		return false
	}
	if !q.After.IsZero() {
		var ts time.Time
		if rec.Task.Status.Timestamp != nil {
			ts = rec.Task.Status.Timestamp.Time
		}
		if !ts.After(q.After) {
			return false
		}
	}
	return true
}

// sortRecsNewestFirst orders by status timestamp descending, then
// updated-at, then id, so equal timestamps (common under a fixed test
// clock) still order deterministically.
func sortRecsNewestFirst(recs []*TaskRecord) {
	sort.Slice(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		ta, tb := statusTime(a), statusTime(b)
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.Task.ID > b.Task.ID
	})
}

func statusTime(rec *TaskRecord) time.Time {
	if rec.Task.Status.Timestamp != nil {
		return rec.Task.Status.Timestamp.Time
	}
	return time.Time{}
}

func (m *MemoryStore) CreatePushConfig(_ context.Context, rec *PushConfigRecord) error {
	if rec == nil {
		return errors.New("a2a: nil push config record")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := pushKey{rec.Owner, rec.Config.TaskID, rec.Config.ID}
	if _, exists := m.push[key]; exists {
		return ErrConflict
	}
	stored := *rec
	stored.Config = clonePushConfig(rec.Config)
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now()
	}
	m.push[key] = &stored
	rec.CreatedAt = stored.CreatedAt
	return nil
}

func (m *MemoryStore) GetPushConfig(_ context.Context, owner, taskID, id string) (*PushConfigRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.push[pushKey{owner, taskID, id}]
	if !ok {
		return nil, ErrNotFound
	}
	out := *rec
	out.Config = clonePushConfig(rec.Config)
	return &out, nil
}

func (m *MemoryStore) ListPushConfigs(_ context.Context, owner, taskID string) ([]*PushConfigRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var recs []*PushConfigRecord
	for key, rec := range m.push {
		// Field equality on the struct key, never a delimiter prefix
		// (see taskKey).
		if key.owner != owner || key.taskID != taskID {
			continue
		}
		out := *rec
		out.Config = clonePushConfig(rec.Config)
		recs = append(recs, &out)
	}
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].CreatedAt.Before(recs[j].CreatedAt)
		}
		return recs[i].Config.ID < recs[j].Config.ID
	})
	return recs, nil
}

func (m *MemoryStore) DeletePushConfig(_ context.Context, owner, taskID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := pushKey{owner, taskID, id}
	if _, ok := m.push[key]; !ok {
		return ErrNotFound
	}
	delete(m.push, key)
	return nil
}

// clonePushConfig copies the config's AuthenticationInfo pointer so two
// callers of GetPushConfig cannot share it.
func clonePushConfig(c PushNotificationConfig) PushNotificationConfig {
	out := c
	if c.Authentication != nil {
		auth := *c.Authentication
		out.Authentication = &auth
	}
	return out
}

// compile-time interface checks.
var (
	_ Store = (*MemoryStore)(nil)
	_ Store = (*SQLStore)(nil)
)
