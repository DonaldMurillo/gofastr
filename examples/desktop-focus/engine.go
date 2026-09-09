package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The timer. State lives in the sessions table (never server RAM): the
// current session is the newest row with completed == false, so a
// restart, a second replica, or a crashed run all recover to the same
// answer. Every read and write goes through the entities' CrudHandlers
// with the caller's identity context, never raw SQL.

// Session phases, as State.Phase reports them.
const (
	phaseIdle   = "idle"
	phaseWork   = "work"
	phaseBreak  = "break"
	phasePaused = "paused"
)

// State is the timer's answer to "what is happening now". It is the
// focus_tick payload and the focus capability's result shape.
type State struct {
	Phase     string `json:"phase"`     // idle | work | break | paused
	Remaining int    `json:"remaining"` // seconds (0 when idle)
	TaskID    string `json:"taskId"`    // "" for an untracked session
	TaskTitle string `json:"taskTitle"` // "" when unknown or untracked
	SessionID string `json:"sessionId"` // "" when idle
	EndsAt    string `json:"endsAt"`    // RFC 3339, "" when idle
}

// Engine drives the pomodoro cycle. All state transitions are
// serialized by mu: the tick loop, the bridge capability, the menu and
// tray handlers, and the deep-link handler can race a completion, and
// the one guard that matters (a work session increments the task's
// completed_pomodoros exactly once) needs one writer at a time.
type Engine struct {
	app *framework.App
	d   *desktop.Battery
	now func() time.Time

	mu sync.Mutex
}

// sessionRow is the typed view of a sessions row as ListAll returns it
// (camelCase keys; timestamps are RFC 3339 strings).
type sessionRow struct {
	ID         string
	TaskID     string
	Kind       string
	StartedAt  time.Time
	EndsAt     time.Time
	PausedAt   time.Time // zero when not paused
	PausedLeft int
	Completed  bool
	Minutes    int
}

// Start begins a work session on taskID (which may be "" for an
// untracked session), opens the floating timer widget, and pushes the
// new state to every open page. Starting while a session is open is
// invalid_input: pause or skip first.
func (e *Engine) Start(ctx context.Context, taskID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	current, err := e.currentSession(ctx)
	if err != nil {
		return err
	}
	if current != nil {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "a session is already running"}
	}
	work := e.d.Preferences().Int("work_minutes")
	n := e.now()
	created, err := e.app.MustCrudHandler("sessions").CreateOne(ctx, map[string]any{
		"task_id":     taskID,
		"kind":        "work",
		"started_at":  stamp(n),
		"ends_at":     stamp(n.Add(time.Duration(work) * time.Minute)),
		"paused_left": 0,
		"completed":   false,
		"minutes":     work,
	})
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	// The floating timer pops with focus. A failure to open it (no
	// window yet, too many windows) must not fail the start.
	spec := desktop.Widget("/widget", 320, 300)
	spec.Style.AllSpaces = true
	if _, err := e.d.OpenWindow(spec); err != nil {
		slog.Warn("desktop-focus: open timer widget", "error", err)
	}
	row := parseSession(created)
	e.emitState(ctx, &row)
	return nil
}

// Pause freezes the running session, storing the seconds that remain.
func (e *Engine) Pause(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.currentSession(ctx)
	if err != nil {
		return err
	}
	if s == nil {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "nothing is running"}
	}
	if !s.PausedAt.IsZero() {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "already paused"}
	}
	left := remainingSeconds(s.EndsAt, e.now())
	updated, err := e.app.MustCrudHandler("sessions").UpdateOne(ctx, s.ID, map[string]any{
		"paused_at":   stamp(e.now()),
		"paused_left": left,
	})
	if err != nil {
		return fmt.Errorf("pause session: %w", err)
	}
	row := parseSession(updated)
	e.emitState(ctx, &row)
	return nil
}

// Resume moves ends_at forward by exactly the stored remaining seconds
// and clears the pause.
func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.currentSession(ctx)
	if err != nil {
		return err
	}
	if s == nil {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "nothing is paused"}
	}
	if s.PausedAt.IsZero() {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "not paused"}
	}
	updated, err := e.app.MustCrudHandler("sessions").UpdateOne(ctx, s.ID, map[string]any{
		"ends_at":     stamp(e.now().Add(time.Duration(s.PausedLeft) * time.Second)),
		"paused_at":   nil,
		"paused_left": 0,
	})
	if err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	row := parseSession(updated)
	e.emitState(ctx, &row)
	return nil
}

// Skip ends the current session now: complete it, then the same
// transition a natural end performs (break after work, idle after
// break) minus the OS notification, because the user just did it
// themselves.
func (e *Engine) Skip(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.currentSession(ctx)
	if err != nil {
		return err
	}
	if s == nil {
		return &desktop.Error{Code: desktop.CodeInvalidInput, Message: "nothing is running"}
	}
	return e.completeSession(ctx, s, false)
}

// Toggle is the tray and menu "Start / Pause" item: idle starts an
// untracked session, running pauses, paused resumes.
func (e *Engine) Toggle(ctx context.Context) error {
	switch st := e.State(ctx); st.Phase {
	case phaseIdle:
		return e.Start(ctx, "")
	case phasePaused:
		return e.Resume(ctx)
	default:
		return e.Pause(ctx)
	}
}

// State computes the timer's current answer.
func (e *Engine) State(ctx context.Context) State {
	s, err := e.currentSession(ctx)
	if err != nil {
		slog.Warn("desktop-focus: read current session", "error", err)
		return State{Phase: phaseIdle}
	}
	if s == nil {
		return State{Phase: phaseIdle}
	}
	st := State{SessionID: s.ID, TaskID: s.TaskID, EndsAt: stamp(s.EndsAt)}
	st.TaskTitle = e.taskTitle(ctx, s.TaskID)
	if !s.PausedAt.IsZero() {
		st.Phase = phasePaused
		st.Remaining = s.PausedLeft
		return st
	}
	st.Phase = s.Kind
	if st.Phase != phaseWork && st.Phase != phaseBreak {
		st.Phase = phaseWork // an unknown kind still runs as work
	}
	st.Remaining = remainingSeconds(s.EndsAt, e.now())
	return st
}

// Tick is one beat of the once-a-second loop: while a session runs it
// updates the tray countdown and pushes the state to every page; when
// the session's time is up it completes the cycle (notify, focus_done,
// auto-start the break after work, idle after a break).
func (e *Engine) Tick(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.currentSession(ctx)
	if err != nil {
		slog.Warn("desktop-focus: tick read", "error", err)
		return
	}
	if s == nil {
		return
	}
	if !s.PausedAt.IsZero() {
		// Paused: the remaining seconds are frozen but pages still
		// sync the phase; the tray keeps whatever it last showed.
		e.emitState(ctx, s)
		return
	}
	if remainingSeconds(s.EndsAt, e.now()) > 0 {
		e.tickRunning(ctx, s)
		return
	}
	if err := e.completeSession(ctx, s, true); err != nil {
		slog.Warn("desktop-focus: complete session", "error", err)
	}
}

// Run is the tick loop: one Tick per second until ctx ends (app
// shutdown). A panic in one tick is recovered and logged, never the
// end of the loop.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tickGuarded(ctx)
		}
	}
}

func (e *Engine) tickGuarded(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("desktop-focus: tick recovered", "panic", textsafe.Recovered(r))
		}
	}()
	e.Tick(localUserCtx(ctx, e.d))
}

// tickRunning updates the tray title (when the owner wants the
// countdown there) and pushes focus_tick with the live state.
func (e *Engine) tickRunning(ctx context.Context, s *sessionRow) {
	if _, ok := e.d.Window(); ok && e.d.Preferences().Bool("tray_countdown") {
		if err := e.d.SetTrayTitle(trayClock(remainingSeconds(s.EndsAt, e.now()))); err != nil {
			slog.Warn("desktop-focus: tray title", "error", err)
		}
	}
	e.emitState(ctx, s)
}

// completeSession performs the end-of-session transition: mark
// completed, count the pomodoro after work, notify (when asked and the
// owner allows), emit focus_done, then start the break after work or
// go idle after a break.
func (e *Engine) completeSession(ctx context.Context, s *sessionRow, notify bool) error {
	if _, err := e.app.MustCrudHandler("sessions").UpdateOne(ctx, s.ID, map[string]any{
		"completed": true,
	}); err != nil {
		return fmt.Errorf("complete session: %w", err)
	}
	if s.Kind == "work" && s.TaskID != "" {
		if err := e.countPomodoro(ctx, s.TaskID); err != nil {
			slog.Warn("desktop-focus: count pomodoro", "error", err)
		}
	}

	var message string
	var nextMinutes int
	prefs := e.d.Preferences()
	switch s.Kind {
	case "work":
		message = fmt.Sprintf("Work session done. Break for %d minutes.", prefs.Int("break_minutes"))
		nextMinutes = prefs.Int("break_minutes")
	default:
		message = "Break over. Back to work."
	}

	// The OS notification is the host's to refuse (an unsigned bundle,
	// a declined permission): unsupported is silence, anything else is
	// a Warn, and no notification failure ever fails the transition.
	if _, ok := e.d.Window(); ok && notify && prefs.Bool("notify_on_done") {
		if err := e.d.Notify(ctx, desktop.Notification{Title: "Focus", Body: message}); err != nil {
			var de *desktop.Error
			if !errors.As(err, &de) || de.Code != desktop.CodeUnsupported {
				slog.Warn("desktop-focus: notify", "error", err)
			}
		}
	}
	e.emit("focus_done", map[string]any{
		"kind":    s.Kind,
		"minutes": s.Minutes,
		"taskId":  s.TaskID,
		"message": message,
	})

	var next *sessionRow
	if s.Kind == "work" {
		n := e.now()
		created, err := e.app.MustCrudHandler("sessions").CreateOne(ctx, map[string]any{
			"task_id":     s.TaskID,
			"kind":        "break",
			"started_at":  stamp(n),
			"ends_at":     stamp(n.Add(time.Duration(nextMinutes) * time.Minute)),
			"paused_left": 0,
			"completed":   false,
			"minutes":     nextMinutes,
		})
		if err != nil {
			return fmt.Errorf("start break: %w", err)
		}
		parsed := parseSession(created)
		next = &parsed
	}
	e.restoreTray(ctx, next)
	e.emitState(ctx, next)
	return nil
}

// countPomodoro increments the task's completed_pomodoros. The read
// and the write both go through the owner-scoped handler, so another
// owner's task is simply not found.
func (e *Engine) countPomodoro(ctx context.Context, taskID string) error {
	tasks := e.app.MustCrudHandler("tasks")
	row, err := tasks.GetOne(ctx, taskID, nil)
	if err != nil {
		return err
	}
	count := asInt(row["completedPomodoros"])
	_, err = tasks.UpdateOne(ctx, taskID, map[string]any{"completed_pomodoros": count + 1})
	return err
}

// restoreTray puts the tray item back to the app name when the timer
// went idle, or to the next session's clock when a break just started
// (both only for owners who asked for the countdown).
func (e *Engine) restoreTray(ctx context.Context, next *sessionRow) {
	if _, ok := e.d.Window(); !ok || !e.d.Preferences().Bool("tray_countdown") {
		return
	}
	title := "Focus"
	if next != nil {
		title = trayClock(remainingSeconds(next.EndsAt, e.now()))
	}
	if err := e.d.SetTrayTitle(title); err != nil {
		slog.Warn("desktop-focus: tray title", "error", err)
	}
}

// emitState pushes focus_tick for the session (nil = idle).
func (e *Engine) emitState(ctx context.Context, s *sessionRow) {
	st := State{Phase: phaseIdle}
	if s != nil {
		st = State{
			Phase:     phaseFromRow(s),
			Remaining: remainingSeconds(s.EndsAt, e.now()),
			TaskID:    s.TaskID,
			TaskTitle: e.taskTitle(ctx, s.TaskID),
			SessionID: s.ID,
			EndsAt:    stamp(s.EndsAt),
		}
		if !s.PausedAt.IsZero() {
			st.Phase = phasePaused
			st.Remaining = s.PausedLeft
		}
	}
	e.emit("focus_tick", st)
}

func (e *Engine) emit(name string, payload any) {
	if _, ok := e.d.Window(); !ok {
		return // --serve mode: no window, no delivery, never an error
	}
	if err := e.d.Emit(name, payload); err != nil {
		slog.Warn("desktop-focus: emit "+name, "error", err)
	}
}

// currentSession returns the newest sessions row with completed ==
// false, or nil when the timer is idle. One query, owner-scoped.
func (e *Engine) currentSession(ctx context.Context) (*sessionRow, error) {
	rows, err := e.app.MustCrudHandler("sessions").ListAll(ctx, crud.ListOptions{
		Limit: 1,
		Sorts: []filter.ParsedSort{{Field: "created_at", Desc: true}},
		Filters: []filter.ParsedFilter{
			(filter.ParsedFilter{Field: "completed", Op: filter.OpEq, Value: "false"}).Coerced(schema.Bool),
		},
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	s := parseSession(rows[0])
	return &s, nil
}

// taskTitle resolves a task's title through the owner-scoped handler;
// unknown, foreign, or untracked is "".
func (e *Engine) taskTitle(ctx context.Context, taskID string) string {
	if taskID == "" {
		return ""
	}
	row, err := e.app.MustCrudHandler("tasks").GetOne(ctx, taskID, nil)
	if err != nil || row == nil {
		return ""
	}
	title, _ := row["title"].(string)
	return title
}

// parseSession converts a CrudHandler row (camelCase keys, RFC 3339
// strings or time.Time values) into the typed view.
func parseSession(row map[string]any) sessionRow {
	var s sessionRow
	s.ID, _ = row["id"].(string)
	s.TaskID, _ = row["taskId"].(string)
	s.Kind, _ = row["kind"].(string)
	s.StartedAt = asTime(row["startedAt"])
	s.EndsAt = asTime(row["endsAt"])
	s.PausedAt = asTime(row["pausedAt"])
	s.PausedLeft = asInt(row["pausedLeft"])
	s.Minutes = asInt(row["minutes"])
	s.Completed, _ = row["completed"].(bool)
	return s
}

func phaseFromRow(s *sessionRow) string {
	if s.Kind == phaseBreak {
		return phaseBreak
	}
	return phaseWork
}

// stamp renders t the way the CRUD layer stores timestamps: an RFC
// 3339 string in UTC, lexicographically sortable.
func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// remainingSeconds is the seconds left until end, floored at 0.
func remainingSeconds(end, now time.Time) int {
	if d := int(end.Sub(now) / time.Second); d > 0 {
		return d
	}
	return 0
}

// trayClock renders seconds as the tray title's mm:ss.
func trayClock(seconds int) string {
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

// asInt reads a column that may arrive as int, int64, or float64 (the
// JSON decode path produces float64).
func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}

// asTime reads a timestamp column that may arrive as an RFC 3339
// string or a time.Time (driver-dependent); nil and unparsable are the
// zero time.
func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
