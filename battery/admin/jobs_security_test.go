package admin

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// The queued run re-authenticates the creator for every chunk under
// their CURRENT roles: an admin whose roles are revoked between confirm
// and drain no longer passes the admin's gate, runs as a plain caller,
// and the gated deletes refuse, even though the confirming request was
// elevated.
func TestBulkRunUsesCurrentRoles(t *testing.T) {
	j := newJobsEnv(t, 120)
	j.setRoles("u1", "admin")

	code, out := j.confirmBulk(t, "u1", 120)
	if code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", code, out)
	}
	jobID, _ := out["job"].(string)

	j.setRoles("u1", "viewer")

	j.q.Start()
	waitFor(t, "the drained job to stop", func() bool { return j.bulkJobStatus(t, jobID) == entityui.BulkStopped })
	if n := j.countPosts(t); n != 120 {
		t.Fatalf("SECURITY: the run deleted %d rows under a revoked role; only %d remain", 120-n, n)
	}
	// A stopped run leaves the job settled: no delete rows were written.
	for _, op := range j.auditOps("posts") {
		if op == "delete" {
			t.Fatal("SECURITY: a revoked-role run wrote delete audit rows")
		}
	}
}

// A creator who is gone when the queue drains stops the run: the
// principal's error is the run's stop, never a run under nobody's
// identity.
func TestBulkRunStopsForGoneCreator(t *testing.T) {
	j := newJobsEnv(t, 120)
	j.setRoles("u1", "admin", "editor")
	code, out := j.confirmBulk(t, "u1", 120)
	if code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", code, out)
	}
	jobID, _ := out["job"].(string)

	j.forgetUser("u1")

	j.q.Start()
	waitFor(t, "the drained job to stop", func() bool { return j.bulkJobStatus(t, jobID) == entityui.BulkStopped })
	if n := j.countPosts(t); n != 120 {
		t.Fatalf("SECURITY: the run deleted %d rows for a deleted creator; only %d remain", 120-n, n)
	}
}

// An admin who holds none of an entity's permissions writes it from the
// admin's screens (elevated), and a queued run of the same bulk action
// writes it too: the run passes the admin's gate again and is elevated,
// the same as the in-request path.
func TestBulkRunAdminIsElevated(t *testing.T) {
	j := newJobsEnv(t, 120)
	j.setRoles("u1", "admin")
	code, out := j.confirmBulk(t, "u1", 120)
	if code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", code, out)
	}
	jobID, _ := out["job"].(string)

	j.q.Start()
	waitFor(t, "the drained job to finish", func() bool { return j.bulkJobStatus(t, jobID) == entityui.BulkDone })
	if n := j.countPosts(t); n != 0 {
		t.Fatalf("the admin's queued run left %d rows", n)
	}
}
