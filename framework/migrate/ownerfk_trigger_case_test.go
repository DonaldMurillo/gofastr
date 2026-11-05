package migrate

import (
	"context"
	"testing"
)

func TestOwnerRepairKeepsCaseTriggers(t *testing.T) {
	for _, table := range []string{"tasks", "Tasks", "TASKS"} {
		t.Run(table, func(t *testing.T) {
			database := legacyDB(t,
				`CREATE TABLE users (id TEXT PRIMARY KEY)`,
				`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT REFERENCES users(id), title TEXT)`,
				`CREATE TABLE audit (id TEXT)`,
				`CREATE TRIGGER task_audit AFTER INSERT ON `+table+` BEGIN INSERT INTO audit VALUES (NEW.id); END`,
			)
			findings, err := FindStaleOwnerForeignKeys(context.Background(), database, testReg{"tasks": taskEntity(t)})
			if err != nil || len(findings) != 1 {
				t.Fatalf("stale keys = %v, %v", findings, err)
			}
			if err := RepairStaleOwnerForeignKeys(context.Background(), database, findings); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`INSERT INTO tasks VALUES ('task-1', 'user-1', 'new')`); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE id = 'task-1'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("trigger wrote %d audit rows after repair, error = %v", count, err)
			}
		})
	}
}
