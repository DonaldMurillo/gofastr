package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The engine against the real entities and handlers, with the clock
// injected: every state transition is a sessions-table transition, and
// every read goes through the owner-scoped CrudHandlers.

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 5, 9, 0, 0, 0, time.Local)}
}

// newEngineApp builds the app without Run (the --serve shape) and
// hands back an engine whose clock is the test's.
func newEngineApp(t *testing.T) (*framework.App, *desktop.Battery, *desktoptest.Shell, *Engine, *fakeClock) {
	t.Helper()
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	shell := desktoptest.NewShell()
	app, d, eng, err := buildApp(shell)
	if err != nil {
		t.Fatal(err)
	}
	// InitPlugins is what Start (and a harness) run; without it the
	// desktop battery's Init never fires and no owner extractor
	// exists for direct engine calls.
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if err := framework.AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	clock := newClock()
	eng.now = clock.now
	return app, d, shell, eng, clock
}

// asOwner returns a context carrying user id, the same shape the
// desktop local identity (or a TestHarness AsUser) installs.
func asOwner(id string) context.Context {
	return handler.SetUser(context.Background(), harnessUser{id: id})
}

// The engine's preference reads answer the DECLARED defaults in this
// no-Run shape (no app state store exists to change them); the harness
// suite covers custom values through preferences.set.

// createTask saves a task through the REST route and returns its id.
func createTask(t *testing.T, app *framework.App, owner, title string) string {
	t.Helper()
	created := framework.TestHarness(t, app).AsUser(harnessUser{id: owner}).
		Post("/api/tasks", map[string]any{"title": title, "estimate": 2}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(created.Body(), &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)
	if id == "" {
		t.Fatalf("task create returned no id: %s", created.Body())
	}
	return id
}

// sessionRows lists the owner's sessions through the API.
func sessionRows(t *testing.T, app *framework.App, owner string) []map[string]any {
	t.Helper()
	body := framework.TestHarness(t, app).AsUser(harnessUser{id: owner}).
		Get("/api/sessions").AssertStatus(t, http.StatusOK).Body()
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatal(err)
	}
	return listed.Data
}

func TestStartCreatesWorkSessionWithDeclaredMinutes(t *testing.T) {
	app, _, _, eng, clock := newEngineApp(t)
	taskID := createTask(t, app, "u1", "Write the brief")

	if err := eng.Start(asOwner("u1"), taskID); err != nil {
		t.Fatal(err)
	}
	rows := sessionRows(t, app, "u1")
	if len(rows) != 1 {
		t.Fatalf("sessions after start = %d, want 1", len(rows))
	}
	row := rows[0]
	if kind, _ := row["kind"].(string); kind != "work" {
		t.Fatalf("kind = %v, want work", row["kind"])
	}
	if minutes := asInt(row["minutes"]); minutes != 25 {
		t.Fatalf("minutes = %v, want 25 (the declared default)", row["minutes"])
	}
	if completed, _ := row["completed"].(bool); completed {
		t.Fatal("a fresh session must not be completed")
	}
	ends, _ := row["endsAt"].(string)
	want := clock.now().Add(25 * time.Minute).UTC().Format(time.RFC3339Nano)
	if ends != want {
		t.Fatalf("endsAt = %q, want %q", ends, want)
	}

	st := eng.State(asOwner("u1"))
	if st.Phase != phaseWork || st.Remaining != 25*60 {
		t.Fatalf("state = %+v, want work/1500s", st)
	}
	if st.TaskID != taskID || st.TaskTitle != "Write the brief" || st.SessionID == "" {
		t.Fatalf("state = %+v", st)
	}
}

func TestStartWhileRunningIsRefused(t *testing.T) {
	_, _, _, eng, _ := newEngineApp(t)
	ctx := asOwner("u1")
	if err := eng.Start(ctx, ""); err != nil {
		t.Fatal(err)
	}
	err := eng.Start(ctx, "")
	var de *desktop.Error
	if !asError(err, &de) || de.Code != desktop.CodeInvalidInput {
		t.Fatalf("second start = %v, want invalid_input", err)
	}
}

func asError(err error, target **desktop.Error) bool {
	if e, ok := err.(*desktop.Error); ok {
		*target = e
		return true
	}
	return false
}

func TestPauseStoresRemainingAndResumeMovesEndsAt(t *testing.T) {
	app, _, _, eng, clock := newEngineApp(t)
	ctx := asOwner("u1")
	if err := eng.Start(ctx, ""); err != nil {
		t.Fatal(err)
	}

	clock.advance(10 * time.Minute) // 25-minute default, 15 left
	if err := eng.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	rows := sessionRows(t, app, "u1")
	if left := asInt(rows[0]["pausedLeft"]); left != 15*60 {
		t.Fatalf("pausedLeft = %v, want 900", rows[0]["pausedLeft"])
	}
	if v, _ := rows[0]["pausedAt"].(string); v == "" {
		t.Fatal("paused_at not stored")
	}
	if st := eng.State(ctx); st.Phase != phasePaused || st.Remaining != 15*60 {
		t.Fatalf("paused state = %+v", st)
	}
	// Pausing again is refused.
	if err := eng.Pause(ctx); err == nil {
		t.Fatal("double pause accepted")
	}

	clock.advance(5 * time.Minute) // wall clock moves; the pause must not
	if err := eng.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	rows = sessionRows(t, app, "u1")
	ends, _ := rows[0]["endsAt"].(string)
	want := clock.now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	if ends != want {
		t.Fatalf("endsAt after resume = %q, want %q (moved by the stored remainder)", ends, want)
	}
	if v, present := rows[0]["pausedAt"]; !present || v != nil {
		t.Fatalf("pausedAt after resume = %v, want cleared", v)
	}
	if st := eng.State(ctx); st.Phase != phaseWork || st.Remaining != 15*60 {
		t.Fatalf("resumed state = %+v", st)
	}
}

func TestTickCompletesWorkIncrementsTaskAndStartsBreak(t *testing.T) {
	app, _, shell, eng, clock := newEngineApp(t)
	taskID := createTask(t, app, "u1", "Ship the timer")
	ctx := asOwner("u1")
	if err := eng.Start(ctx, taskID); err != nil {
		t.Fatal(err)
	}

	// The declared defaults are 25 minutes of work and 5 of break;
	// the clock is injected, so waiting them out costs nothing.
	clock.advance(25*time.Minute + time.Second)
	eng.Tick(ctx)

	rows := sessionRows(t, app, "u1")
	if len(rows) != 2 {
		t.Fatalf("sessions after the work end = %d, want 2 (work + auto break): %+v", len(rows), rows)
	}
	var work, brk map[string]any
	for _, r := range rows {
		switch r["kind"] {
		case "work":
			work = r
		case "break":
			brk = r
		}
	}
	if work == nil || brk == nil {
		t.Fatalf("sessions after the work end = %+v, want one work and one break", rows)
	}
	if completed, _ := work["completed"].(bool); !completed {
		t.Fatal("the work session is not completed")
	}
	if kind, _ := brk["kind"].(string); kind != "break" {
		t.Fatalf("the auto-started session kind = %v, want break", brk["kind"])
	}
	if minutes := asInt(brk["minutes"]); minutes != 5 {
		t.Fatalf("break minutes = %v, want 5 (the declared default)", brk["minutes"])
	}
	// The task counted one pomodoro.
	taskBody := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"}).
		Get("/api/tasks/"+taskID).AssertStatus(t, http.StatusOK).Body()
	if !strings.Contains(taskBody, `"completedPomodoros":1`) {
		t.Fatalf("task after the work end: %s", taskBody)
	}
	// No window in this shape, so no OS notification and no events.
	if n := len(shell.Notifications()); n != 0 {
		t.Fatalf("notifications without a window = %d", n)
	}
	if st := eng.State(ctx); st.Phase != phaseBreak {
		t.Fatalf("state after the work end = %+v, want break", st)
	}

	// The break runs out: idle, tray back to the app name, and the
	// task is NOT incremented again.
	clock.advance(5*time.Minute + time.Second)
	eng.Tick(ctx)
	rows = sessionRows(t, app, "u1")
	if completed, _ := rows[0]["completed"].(bool); !completed {
		t.Fatal("the break session is not completed")
	}
	if st := eng.State(ctx); st.Phase != phaseIdle || st.SessionID != "" {
		t.Fatalf("state after the break end = %+v, want idle", st)
	}
	if !strings.Contains(taskBody, `"completedPomodoros":1`) {
		t.Fatal("unreachable")
	}
}

func TestSkipEndsTheSessionWithoutNotifying(t *testing.T) {
	app, _, shell, eng, clock := newEngineApp(t)
	ctx := asOwner("u1")
	if err := eng.Start(ctx, ""); err != nil {
		t.Fatal(err)
	}

	clock.advance(30 * time.Second)
	if err := eng.Skip(ctx); err != nil {
		t.Fatal(err)
	}
	rows := sessionRows(t, app, "u1")
	if len(rows) != 2 {
		t.Fatalf("sessions after skip = %d, want 2", len(rows))
	}
	var work, brk map[string]any
	for _, r := range rows {
		switch r["kind"] {
		case "work":
			work = r
		case "break":
			brk = r
		}
	}
	if completed, _ := work["completed"].(bool); !completed {
		t.Fatal("skip did not complete the work session")
	}
	if brk == nil {
		t.Fatalf("after skip there is no break session: %+v", rows)
	}
	// The user ended it themselves: no OS notification.
	if n := len(shell.Notifications()); n != 0 {
		t.Fatalf("skip notifications = %d, want 0", n)
	}
	// Skipping the break goes idle.
	if err := eng.Skip(ctx); err != nil {
		t.Fatal(err)
	}
	if st := eng.State(ctx); st.Phase != phaseIdle {
		t.Fatalf("state after skipping the break = %+v", st)
	}
}

func TestTwoOwnersNeverSeeEachOthersSessions(t *testing.T) {
	app, _, _, eng, _ := newEngineApp(t)
	u1 := createTask(t, app, "u1", "u1 deep work")
	if err := eng.Start(asOwner("u1"), u1); err != nil {
		t.Fatal(err)
	}

	// u2's engine is idle and starting creates u2's OWN session.
	if st := eng.State(asOwner("u2")); st.Phase != phaseIdle {
		t.Fatalf("u2 sees u1's session: %+v", st)
	}
	u2 := createTask(t, app, "u2", "u2 own work")
	if err := eng.Start(asOwner("u2"), u2); err != nil {
		t.Fatal(err)
	}
	if got := len(sessionRows(t, app, "u1")); got != 1 {
		t.Fatalf("u1 sessions = %d, want 1", got)
	}
	if got := len(sessionRows(t, app, "u2")); got != 1 {
		t.Fatalf("u2 sessions = %d, want 1", got)
	}

	// u2's tick completes only u2's session.
	clock := newClock()
	_ = clock
	eng.now = func() time.Time { return time.Now().Add(time.Hour) }
	eng.Tick(asOwner("u2"))
	if got := len(sessionRows(t, app, "u1")); got != 1 {
		t.Fatalf("u1 sessions after u2's tick = %d, want 1", got)
	}
	u2rows := sessionRows(t, app, "u2")
	if completed, _ := u2rows[0]["completed"].(bool); !completed {
		t.Fatal("u2's own session did not complete on u2's tick")
	}
	if st := eng.State(asOwner("u1")); st.Phase != phaseWork {
		t.Fatalf("u1's session was disturbed by u2's tick: %+v", st)
	}
}
