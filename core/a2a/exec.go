package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// cloneTask deep-copies a Task so a caller mutating a returned record
// cannot change what the store or a run holds. The copy goes through
// JSON because Task carries maps and pointers (Part.Data is an *any that
// may hold decoded maps). A Task holding a value JSON cannot marshal
// (a channel smuggled into metadata by a handler) falls back to a
// shallow copy: the same value would fail store persistence anyway, so
// the failure surfaces there instead of here.
func cloneTask(t Task) Task {
	b, err := json.Marshal(t)
	if err != nil {
		return t
	}
	var out Task
	if err := json.Unmarshal(b, &out); err != nil {
		return t
	}
	return out
}

// cloneMessage deep-copies a Message, for the same reason as cloneTask.
func cloneMessage(m Message) Message {
	b, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out Message
	if err := json.Unmarshal(b, &out); err != nil {
		return m
	}
	return out
}

// Clone deep-copies the record: the Task via cloneTask, the scalars
// plainly.
func (r *TaskRecord) Clone() *TaskRecord {
	c := *r
	c.Task = cloneTask(r.Task)
	return &c
}

// appendHistory appends m to the task's history, capped at max: the
// OLDEST entries are dropped, never the newest, so the message the
// client just sent and the agent's latest answer always survive.
func appendHistory(task *Task, m *Message, max int) {
	if m == nil {
		return
	}
	task.History = append(task.History, *m)
	if max > 0 && len(task.History) > max {
		task.History = task.History[len(task.History)-max:]
	}
}

// taskRun is the TaskContext a handler sees, and the state machine the
// handler drives. Its mutex serializes every mutation so a handler
// calling into the TaskContext from two goroutines cannot interleave a
// persist with a publish.
type taskRun struct {
	srv   *Server
	owner string
	req   *http.Request

	mu sync.Mutex
	// rec is this run's view of the stored record. It advances only
	// after Store.UpdateTask accepts the version, so a competing write
	// (CancelTask, another replica) makes the next mutation fail with a
	// conflict instead of silently forking the task.
	rec *TaskRecord
	msg *Message
	// interrupted is set when this run paused the task
	// (RequireInput/RequireAuth). Working after that is an error: the
	// handler already handed control back to the client.
	interrupted bool
}

func newTaskRun(s *Server, rec *TaskRecord, msg *Message, r *http.Request, owner string) *taskRun {
	return &taskRun{srv: s, rec: rec, msg: msg, req: r, owner: owner}
}

// Task returns a snapshot of the task as currently persisted. The
// snapshot is deep: mutating it cannot corrupt the run.
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

// taskID reads the id under the lock: a handler may still be calling
// into the TaskContext from a goroutine it spawned when the runner logs.
func (t *taskRun) taskID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rec.Task.ID
}

// storeOpTimeout bounds one Store call made on the run's behalf. The
// run context is deliberately not used (a canceled run must still
// persist its final state), so this is the only thing standing between
// a wedged database and a goroutine that never returns.
const storeOpTimeout = 30 * time.Second

// Request returns the HTTP request that started or resumed this run.
// Its Context is deliberately not the handler's context, so handlers
// re-dispatching into the app with the caller's credentials do not
// inherit the run's timeout.
func (t *taskRun) Request() *http.Request { return t.req }

// Working moves the task to TASK_STATE_WORKING with an optional agent
// status message. Idempotent once working.
func (t *taskRun) Working(parts ...Part) error {
	return t.mutate(func(task *Task) (*StreamResponse, error) {
		switch state := task.Status.State; {
		case state.Terminal():
			return nil, errTerminalRefusal(task.ID, state, TaskStateWorking)
		case state == TaskStateWorking:
			// Already announced; repeating would only duplicate the
			// event on every stream.
			return nil, nil
		case t.interrupted:
			return nil, errors.New("a2a: task paused for input in this run; Working refused until resumed")
		}
		return t.setStatusInPlace(task, TaskStateWorking, parts), nil
	})
}

// Artifact records an artifact and emits an artifact-update event.
func (t *taskRun) Artifact(a Artifact, appendParts bool) error {
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
				task.Artifacts[i].Parts = append(task.Artifacts[i].Parts, a.Parts...)
			} else {
				task.Artifacts[i] = a
			}
			break
		}
		if !found {
			task.Artifacts = append(task.Artifacts, a)
		}
		// The event carries what the client must apply, and for an
		// append that is the DELTA: spec §7.2 says a receiver appends
		// the event's parts to the artifact it already holds, so sending
		// the merged artifact with append=true would double every part
		// already delivered. The stored task holds the merged result.
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

func errTerminalRefusal(id string, state, want TaskState) error {
	return fmt.Errorf("a2a: task %s is %s; refusing transition to %s", id, state, want)
}

func (t *taskRun) terminalFn(state TaskState, parts []Part) func(*Task) (*StreamResponse, error) {
	return func(task *Task) (*StreamResponse, error) {
		if cur := task.Status.State; cur.Terminal() {
			return nil, errTerminalRefusal(task.ID, cur, state)
		}
		return t.setStatusInPlace(task, state, parts), nil
	}
}

func (t *taskRun) pauseFn(state TaskState, parts []Part) func(*Task) (*StreamResponse, error) {
	return func(task *Task) (*StreamResponse, error) {
		if cur := task.Status.State; cur.Terminal() {
			return nil, errTerminalRefusal(task.ID, cur, state)
		}
		t.interrupted = true
		return t.setStatusInPlace(task, state, parts), nil
	}
}

// setStatusInPlace writes the new status onto task (state, optional
// agent status message appended to history, fresh timestamp) and
// returns the status-update event for it. The caller persists.
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

// resumeWorking is the resume transition: append the new message, move
// to WORKING, publish.
func (t *taskRun) resumeWorking(msg *Message) error {
	return t.mutate(func(task *Task) (*StreamResponse, error) {
		if state := task.Status.State; state.Terminal() {
			return nil, errTerminalRefusal(task.ID, state, TaskStateWorking)
		}
		in := *msg
		in.TaskID = task.ID
		in.ContextID = task.ContextID
		task.History = append(task.History, in)
		if t.srv.maxHist > 0 && len(task.History) > t.srv.maxHist {
			task.History = task.History[len(task.History)-t.srv.maxHist:]
		}
		task.Status = TaskStatus{State: TaskStateWorking, Timestamp: t.srv.stamp()}
		ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    task.ID,
			ContextID: task.ContextID,
			Status:    task.Status,
		}}
		return &ev, nil
	})
}

// mutate is the one path every TaskContext mutation goes through:
// build the next task state on a copy, persist it with the version
// check, and only then publish to subscribers and push configs. Two
// invariants follow. A subscriber is never told something the store
// does not hold (persist-before-publish), and a mutation that loses the
// version race (the task was canceled under us) changes nothing — the
// run's in-memory copy stays at the refused candidate's parent, so the
// NEXT call fails the same way instead of building on a write the store
// rejected.
func (t *taskRun) mutate(build func(task *Task) (*StreamResponse, error)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	cand := t.rec.Clone()
	ev, err := build(&cand.Task)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := t.srv.store.UpdateTask(ctx, cand); err != nil {
		if errors.Is(err, ErrConflict) {
			// The store moved on without us: the task was canceled or
			// another replica resumed it. This run's writes are done.
			return fmt.Errorf("a2a: task %s changed concurrently (canceled elsewhere?); update refused", t.rec.Task.ID)
		}
		return fmt.Errorf("a2a: persist task %s: %w", t.rec.Task.ID, err)
	}
	t.rec = cand
	if ev != nil {
		t.srv.publish(t.owner, t.rec, *ev)
	}
	return nil
}

// invoke runs the skill handler, converting a panic into a failure so
// one poison skill cannot take the process down. The panic is logged
// with its stack; the task gets a generic message (the panic value can
// be anything, including an internal error string).
var errHandlerPanicked = errors.New("a2a: skill handler panicked")

func (s *Server) invoke(ctx context.Context, t *taskRun, h Handler) (err error) {
	taskID := t.taskID()
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("a2a: skill handler panicked",
				"taskId", taskID, "panic", p, "stack", string(debug.Stack()))
			err = errHandlerPanicked
		}
	}()
	return h(ctx, t)
}

// finalize settles the task after the handler returns. Terminal and
// interrupted states are the handler's own last word and stand.
// Everything else is decided by why the handler returned: deadline →
// FAILED "task timed out", cancellation → CANCELED (CancelTask already
// wrote it; this only matters if something else canceled the context),
// error → FAILED, clean return → COMPLETED.
//
// The decision is made against the STORE, not the run's possibly stale
// copy: after CancelTask the local copy still says WORKING, and it must
// not resurrect a task the client was told was canceled.
func (s *Server) finalize(ctx context.Context, t *taskRun, err error) {
	t.mu.Lock()
	local := t.rec.Task.Status.State
	t.mu.Unlock()

	var final TaskState
	var msg string
	switch {
	case local.Terminal(), local.Interrupted():
		return
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		final, msg = TaskStateFailed, "task timed out"
	case ctx.Err() != nil:
		final = TaskStateCanceled
	case err != nil:
		final, msg = TaskStateFailed, "skill handler failed"
		var ae *Error
		if errors.As(err, &ae) {
			// The handler chose this message; it is client-safe.
			msg = ae.Message
		} else {
			// Not an *Error the handler chose: log the real text, tell
			// the client only that the skill failed.
			s.log.Error("a2a: skill handler failed", "taskId", t.taskID(), "err", err)
		}
	default:
		final = TaskStateCompleted
	}
	t.setFinalAgainstStore(final, msg)
}

// setFinalAgainstStore writes a final state via read-modify-write with
// bounded retries, so a concurrent CancelTask cannot be silently
// overwritten by a late completion and vice versa: whichever write lands
// first makes the task terminal, and the loser's re-read sees a
// terminal state and leaves it alone.
func (t *taskRun) setFinalAgainstStore(state TaskState, msgText string) {
	const attempts = 8
	taskID := t.taskID()
	// attempt returns done=true when the loop should stop: the task was
	// already settled, the write landed, or a non-conflict error ended
	// it. A conflict returns false so the caller re-reads.
	attempt := func() (done bool) {
		ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
		defer cancel()
		rec, err := t.srv.store.GetTask(ctx, t.owner, taskID)
		if err != nil {
			t.srv.log.Error("a2a: finalize read", "taskId", taskID, "err", err)
			return true
		}
		if cur := rec.Task.Status.State; cur.Terminal() || cur.Interrupted() {
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
		return true
	}
	for range attempts {
		if attempt() {
			return
		}
	}
	t.srv.log.Error("a2a: finalize gave up after repeated conflicts", "taskId", taskID)
}

// stamp is the injectable clock entry: one place to make timestamps
// deterministic in tests.
func (s *Server) stamp() *Timestamp {
	return &Timestamp{s.now().UTC().Truncate(time.Millisecond)}
}
