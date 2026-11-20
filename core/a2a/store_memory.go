package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore is the in-process Store: a mutex, two maps, deep copies at the
// boundary. It is per process; use SQLStore to share tasks across replicas.
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

type taskKey struct{ owner, id string }

type pushKey struct{ owner, taskID, id string }

func (m *MemoryStore) CreateTask(_ context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	if _, err := json.Marshal(rec.Task); err != nil {
		return fmt.Errorf("a2a: marshal task: %w", err)
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
		return nil, ErrNotFound
	}
	return rec.Clone(), nil
}

func (m *MemoryStore) UpdateTask(_ context.Context, rec *TaskRecord) error {
	if rec == nil {
		return errors.New("a2a: nil task record")
	}
	if _, err := json.Marshal(rec.Task); err != nil {
		return fmt.Errorf("a2a: marshal task: %w", err)
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
	next.Tenant = stored.Tenant
	next.Scope = stored.Scope
	next.UpdatedAt = time.Now()
	m.tasks[key] = next
	*rec = *next.Clone()
	return nil
}

// FiltersByScope implements ScopeFilter: ListTasks filters by
// ListQuery.Scoped in its query.
func (m *MemoryStore) FiltersByScope() {}

// FiltersByTenant implements TenantFilter: ListTasks filters by
// ListQuery.Tenanted in its query.
func (m *MemoryStore) FiltersByTenant() {}

func (m *MemoryStore) ListTasks(_ context.Context, owner string, q ListQuery) (recs []*TaskRecord, total int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []*TaskRecord
	for key, rec := range m.tasks {
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

func matchesQuery(rec *TaskRecord, q ListQuery) bool {
	if q.ContextID != "" && rec.Task.ContextID != q.ContextID {
		return false
	}
	if q.Scoped && rec.Scope != q.Scope {
		return false
	}
	if q.Tenanted && rec.Tenant != q.Tenant {
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

const orphanPushGrace = 10 * time.Minute

func (m *MemoryStore) TrimTerminalTasks(_ context.Context, owner string, tier RetentionTier, keep int) error {
	if keep < 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var terminal []*TaskRecord
	for key, rec := range m.tasks {
		if key.owner == owner && tier.holds(rec) && rec.Task.Status.State.Terminal() {
			terminal = append(terminal, rec)
		}
	}
	if len(terminal) <= keep {
		return nil
	}
	sortRecsNewestFirst(terminal)
	trimmed := make(map[string]bool, len(terminal)-keep)
	for _, rec := range terminal[keep:] {
		delete(m.tasks, taskKey{owner, rec.Task.ID})
		trimmed[rec.Task.ID] = true
	}
	cutoff := time.Now().Add(-orphanPushGrace)
	for key, rec := range m.push {
		if key.owner != owner {
			continue
		}
		if _, live := m.tasks[taskKey{owner, key.taskID}]; trimmed[key.taskID] || (!live && rec.CreatedAt.Before(cutoff)) {
			delete(m.push, key)
		}
	}
	return nil
}

// TrimPushConfigs implements RetentionTrimmer: it keeps the newest
// `keep` configs for (owner, taskID) — the same order ListPushConfigs
// serves — and deletes the older ones.
func (m *MemoryStore) TrimPushConfigs(_ context.Context, owner, taskID string, keep int) error {
	if keep < 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var recs []*PushConfigRecord
	for key, rec := range m.push {
		if key.owner == owner && key.taskID == taskID {
			recs = append(recs, rec)
		}
	}
	if len(recs) <= keep {
		return nil
	}
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].CreatedAt.Before(recs[j].CreatedAt)
		}
		return recs[i].Config.ID < recs[j].Config.ID
	})
	for _, rec := range recs[:len(recs)-keep] {
		delete(m.push, pushKey{owner, taskID, rec.Config.ID})
	}
	return nil
}

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
	_ Store            = (*MemoryStore)(nil)
	_ Store            = (*SQLStore)(nil)
	_ RetentionTrimmer = (*MemoryStore)(nil)
	_ RetentionTrimmer = (*SQLStore)(nil)
)
