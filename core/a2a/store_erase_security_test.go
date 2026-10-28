package a2a_test

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Erasing a user deletes their A2A tasks and push configs, under the
// configured table names, and a data export carries both tables (S20).
func TestEraseRemovesA2AState(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	st, err := a2a.NewSQLStore(db, a2a.WithTablePrefix("svc_"))
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	for _, owner := range []string{"victim", "bystander"} {
		id := "task-" + owner
		if err := st.CreateTask(ctx, &a2a.TaskRecord{
			Task:  a2a.Task{ID: id, Status: a2a.TaskStatus{State: "TASK_STATE_COMPLETED", Timestamp: &a2a.Timestamp{Time: now}}},
			Owner: owner,
		}); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if err := st.CreatePushConfig(ctx, &a2a.PushConfigRecord{
			Config: a2a.PushNotificationConfig{ID: "cfg-" + owner, TaskID: id, URL: "https://example.com/hook"},
			Owner:  owner,
		}); err != nil {
			t.Fatalf("CreatePushConfig: %v", err)
		}
	}

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	for _, name := range []string{"a2a_tasks-svc_a2a_tasks", "a2a_push_configs-svc_a2a_push_configs"} {
		if n := archiveLines(t, dir, name); n != 2 {
			t.Errorf("export %s holds %d row(s), want 2", name, n)
		}
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if _, err := st.GetTask(ctx, "victim", "task-victim"); err == nil {
		t.Error("the erased user's task is still stored")
	}
	if _, err := st.GetPushConfig(ctx, "victim", "task-victim", "cfg-victim"); err == nil {
		t.Error("the erased user's push config is still stored")
	}
	if _, err := st.GetTask(ctx, "bystander", "task-bystander"); err != nil {
		t.Errorf("another user's task: %v", err)
	}
	if _, err := st.GetPushConfig(ctx, "bystander", "task-bystander", "cfg-bystander"); err != nil {
		t.Errorf("another user's push config: %v", err)
	}
}

// archiveLines counts the rows in the archive file for name.
func archiveLines(t *testing.T, dir, name string) int {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name+".ndjson"))
	if err != nil {
		t.Errorf("archive has no %s: %v", name, err)
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		t.Errorf("read %s: %v", name, err)
	}
	return n
}
