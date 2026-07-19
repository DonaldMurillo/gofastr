package queue

import (
	"context"
	"testing"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestQueueReadsPropagateInvalidTime(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(context.Background(), Job{ID: "bad-time", Type: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE " + q.qt() + " SET created_at='not-a-time' WHERE id='bad-time'"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ListJobs(context.Background(), "", 10); err == nil {
		t.Fatal("ListJobs hid malformed created_at")
	}
}
