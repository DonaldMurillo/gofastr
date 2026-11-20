package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

func jsonClone[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out T
	if err := jsonexact.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func cloneTask(t Task) Task { return jsonClone(t) }

func cloneMessage(m Message) Message { return jsonClone(m) }

// Clone deep-copies the record: the Task via cloneTask, the scalars
// plainly.
func (r *TaskRecord) Clone() *TaskRecord {
	c := *r
	c.Task = cloneTask(r.Task)
	return &c
}

func appendHistory(task *Task, m *Message, max int) {
	if m == nil {
		return
	}
	task.History = append(task.History, *m)
	if max > 0 && len(task.History) > max {
		task.History = task.History[len(task.History)-max:]
	}
}

type taskRun struct {
	srv   *Server
	owner string
	req   *http.Request

	mu          sync.Mutex
	rec         *TaskRecord
	msg         *Message
	interrupted bool
	rn          *run
	runCtx      context.Context
}

func (t *taskRun) attach(rn *run, runCtx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rn = rn
	t.runCtx = runCtx
}

func (t *taskRun) haltedLocked() bool { return t.rn != nil && t.rn.halted.Load() }

func newTaskRun(s *Server, rec *TaskRecord, msg *Message, r *http.Request, owner string) *taskRun {
	return &taskRun{srv: s, rec: rec, msg: msg, req: r, owner: owner}
}

// Task returns a snapshot of the task as currently persisted.
func (t *taskRun) Task() *Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	task := cloneTask(t.rec.Task)
	return &task
}

// Message returns the message this run was started or resumed with.
func (t *taskRun) Message() *Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	msg := cloneMessage(*t.msg)
	return &msg
}

// Owner returns the principal that owns the task.
func (t *taskRun) Owner() string { return t.owner }

func (t *taskRun) taskID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rec.Task.ID
}

var storeOpTimeout = 30 * time.Second

// Request returns the HTTP request that started or resumed this run.
func (t *taskRun) Request() *http.Request { return t.req }

// Working moves the task to TASK_STATE_WORKING with an optional agent status
// message.
func (t *taskRun) Working(parts ...Part) error {
	if err := validateParts(parts); err != nil {
		return err
	}
	return t.mutate(func(task *Task) (*StreamResponse, error) {
		switch state := task.Status.State; {
		case state.Terminal():
			return nil, errTerminalRefusal(task.ID, state, TaskStateWorking)
		case state == TaskStateWorking:
			return nil, nil
		case t.interrupted:
			return nil, errors.New("a2a: task paused for input in this run; Working refused until resumed")
		}
		return t.setStatusInPlace(task, TaskStateWorking, parts), nil
	})
}

// Artifact records an artifact and emits an artifact-update event.
func (t *taskRun) Artifact(a Artifact, appendParts bool) error {
	if a.Parts == nil {
		a.Parts = []Part{}
	}
	if err := validateParts(a.Parts); err != nil {
		return err
	}
	return t.mutate(func(task *Task) (*StreamResponse, error) {
		if state := task.Status.State; state.Terminal() {
			return nil, errTerminalRefusal(task.ID, state, TaskStateWorking)
		}
		if a.ArtifactID == "" {
			a.ArtifactID = t.srv.newID()
		}
		found := false
		for i := range task.Artifacts {
			if task.Artifacts[i].ArtifactID != a.ArtifactID {
				continue
			}
			found = true
			if appendParts {
				parts := task.Artifacts[i].Parts
				if parts == nil {
					parts = []Part{}
				}
				task.Artifacts[i].Parts = append(parts, a.Parts...)
			} else {
				task.Artifacts[i] = a
			}
			break
		}
		if !found {
			task.Artifacts = append(task.Artifacts, a)
		}
		ev := StreamResponse{ArtifactUpdate: &TaskArtifactUpdateEvent{
			TaskID:    task.ID,
			ContextID: task.ContextID,
			Artifact:  a,
			Append:    appendParts && found,
		}}
		return &ev, nil
	})
}

// Complete ends the task in TASK_STATE_COMPLETED.
func (t *taskRun) Complete(parts ...Part) error {
	return t.mutate(t.terminalFn(TaskStateCompleted, parts))
}

// Fail ends the task in TASK_STATE_FAILED.
func (t *taskRun) Fail(parts ...Part) error {
	return t.mutate(t.terminalFn(TaskStateFailed, parts))
}

// Reject ends the task in TASK_STATE_REJECTED.
func (t *taskRun) Reject(parts ...Part) error {
	return t.mutate(t.terminalFn(TaskStateRejected, parts))
}

// RequireInput pauses the task in TASK_STATE_INPUT_REQUIRED.
func (t *taskRun) RequireInput(parts ...Part) error {
	return t.mutate(t.pauseFn(TaskStateInputRequired, parts))
}

// RequireAuth pauses the task in TASK_STATE_AUTH_REQUIRED.
func (t *taskRun) RequireAuth(parts ...Part) error {
	return t.mutate(t.pauseFn(TaskStateAuthRequired, parts))
}

var errConcurrentChange = errors.New("a2a: task changed concurrently")

func errTerminalRefusal(id string, state, want TaskState) error {
	return fmt.Errorf("a2a: task %s is %s; refusing transition to %s", id, state, want)
}

func (t *taskRun) terminalFn(state TaskState, parts []Part) func(*Task) (*StreamResponse, error) {
	return func(task *Task) (*StreamResponse, error) {
		if err := validateParts(parts); err != nil {
			return nil, err
		}
		if cur := task.Status.State; cur.Terminal() {
			return nil, errTerminalRefusal(task.ID, cur, state)
		}
		return t.setStatusInPlace(task, state, parts), nil
	}
}

func (t *taskRun) pauseFn(state TaskState, parts []Part) func(*Task) (*StreamResponse, error) {
	return func(task *Task) (*StreamResponse, error) {
		if err := validateParts(parts); err != nil {
			return nil, err
		}
		if cur := task.Status.State; cur.Terminal() {
			return nil, errTerminalRefusal(task.ID, cur, state)
		}
		t.interrupted = true
		return t.setStatusInPlace(task, state, parts), nil
	}
}

func (t *taskRun) setStatusInPlace(task *Task, state TaskState, parts []Part) *StreamResponse {
	var msg *Message
	if len(parts) > 0 {
		msg = &Message{
			MessageID: t.srv.newID(),
			ContextID: task.ContextID,
			TaskID:    task.ID,
			Role:      RoleAgent,
			Parts:     parts,
		}
		appendHistory(task, msg, t.srv.maxHist)
	}
	task.Status = TaskStatus{State: state, Message: msg, Timestamp: t.srv.stamp()}
	ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
		TaskID:    task.ID,
		ContextID: task.ContextID,
		Status:    task.Status,
	}}
	return &ev
}

func (t *taskRun) resumeWorking(msg *Message) error {
	return t.mutate(func(task *Task) (*StreamResponse, error) {
		if state := task.Status.State; state.Terminal() {
			return nil, fmt.Errorf("%w: task %s is %s", errConcurrentChange, task.ID, state)
		}
		in := *msg
		in.TaskID = task.ID
		in.ContextID = task.ContextID
		appendHistory(task, &in, t.srv.maxHist)
		task.Status = TaskStatus{State: TaskStateWorking, Timestamp: t.srv.stamp()}
		ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    task.ID,
			ContextID: task.ContextID,
			Status:    task.Status,
		}}
		return &ev, nil
	})
}

func (t *taskRun) mutate(build func(task *Task) (*StreamResponse, error)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.haltedLocked() {
		return fmt.Errorf("%w: task %s is being canceled", errConcurrentChange, t.rec.Task.ID)
	}
	if t.runCtx != nil && errors.Is(t.runCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("a2a: task %s timed out: %w", t.rec.Task.ID, context.DeadlineExceeded)
	}
	cand := t.rec.Clone()
	ev, err := build(&cand.Task)
	if err != nil {
		return err
	}
	if ev == nil && reflect.DeepEqual(cand.Task, cloneTask(t.rec.Task)) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := t.srv.store.UpdateTask(ctx, cand); err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%w: task %s update refused (canceled or resumed elsewhere)", errConcurrentChange, t.rec.Task.ID)
		}
		return fmt.Errorf("a2a: persist task %s: %w", t.rec.Task.ID, err)
	}
	t.rec = cand
	if t.interrupted && t.rn != nil {
		t.rn.pausedAt.Store(cand.Version)
	}
	if ev != nil {
		t.srv.publish(t.owner, t.rec, *ev)
	}
	switch state := cand.Task.Status.State; {
	case state.Terminal():
		t.srv.trimTerminalTasks(t.owner, TierOf(cand))
	case state.Interrupted():
		t.srv.trimPausedTasks(t.owner, TierOf(cand))
	}
	return nil
}

var errHandlerPanicked = errors.New("a2a: skill handler panicked")

func (s *Server) invoke(ctx context.Context, t *taskRun, h Handler) (err error) {
	taskID := t.taskID()
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("a2a: skill handler panicked",
				"taskId", taskID, "panic", textsafe.Recovered(p), "stack", string(debug.Stack()))
			err = errHandlerPanicked
		}
	}()
	return h(ctx, t)
}

func (s *Server) finalize(ctx context.Context, t *taskRun, err error) {
	t.mu.Lock()
	local := t.rec.Task.Status.State
	halted := t.haltedLocked()
	t.mu.Unlock()

	var final TaskState
	var msg string
	switch {
	case local.Terminal(), local.Interrupted():
		return
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		final, msg = TaskStateFailed, "task timed out"
	case ctx.Err() != nil, halted:
		final = TaskStateCanceled
	case err != nil:
		final, msg = TaskStateFailed, "skill handler failed"
		var ae *Error
		if errors.As(err, &ae) {
			msg = ae.Message
		} else {
			s.log.Error("a2a: skill handler failed", "taskId", t.taskID(), "err", err)
		}
	default:
		final = TaskStateCompleted
	}
	t.setFinalAgainstStore(final, msg)
}

func (t *taskRun) setFinalAgainstStore(state TaskState, msgText string) {
	const attempts = 8
	taskID := t.taskID()
	attempt := func() (done bool) {
		ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
		defer cancel()
		rec, err := t.srv.store.GetTask(ctx, t.owner, taskID)
		if err != nil {
			t.srv.log.Error("a2a: finalize read", "taskId", taskID, "err", err)
			return true
		}
		if cur := rec.Task.Status.State; cur.Terminal() || cur.Interrupted() {
			t.mu.Lock()
			announce := t.rn != nil && !t.haltedLocked() && cur.Terminal() && rec.Version > t.rec.Version
			t.rec = rec
			if announce {
				t.rn.bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
					TaskID:    rec.Task.ID,
					ContextID: rec.Task.ContextID,
					Status:    rec.Task.Status,
				}}, version: rec.Version})
			}
			t.mu.Unlock()
			return true
		}
		cand := rec.Clone()
		var parts []Part
		if msgText != "" {
			parts = []Part{TextPart(msgText)}
		}
		t.mu.Lock()
		ev := t.setStatusInPlace(&cand.Task, state, parts)
		t.mu.Unlock()
		if err := t.srv.store.UpdateTask(ctx, cand); err != nil {
			if errors.Is(err, ErrConflict) {
				return false
			}
			t.srv.log.Error("a2a: finalize write", "taskId", taskID, "err", err)
			return true
		}
		t.mu.Lock()
		t.rec = cand
		t.mu.Unlock()
		t.srv.publish(t.owner, cand, *ev)
		t.srv.trimTerminalTasks(t.owner, TierOf(cand))
		return true
	}
	for range attempts {
		if attempt() {
			return
		}
	}
	t.srv.log.Error("a2a: finalize gave up after repeated conflicts", "taskId", taskID)
}

func (s *Server) stamp() *Timestamp {
	return &Timestamp{s.now().UTC().Truncate(time.Millisecond)}
}
