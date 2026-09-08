package admin

// Pins: every mutating admin RPC appends an audit row — queue replay included (round 5, fixed).
// CONTRACT-QUESTION red: the package doc enumerates audited mutations without naming queue
// replay; this asserts the forensic-completeness policy the other mutating RPCs already follow.
// Property: every mutating admin RPC appends an audit row — grant/revoke/assign
// (rbac_admin.go:409/:441/:491, incl. refused branches :391/:431/:481) and all four module
// lifecycle ops (process_modules.go:303/:321/:340/:361); entity writes audit via CrudHandler
// hooks. handleQueueReplay re-queues a dead-lettered job with NO audit row, though its own
// doc comment calls an ungated replay "a privilege-escalation / job-amplification vector".
// Surfaces: battery/admin/admin.go::handleQueueReplay (:586, route :365).
// Finding: POST <prefix>/queue/_replay/{id} from an authenticated admin fires
// queue.Replayable.Replay and redirects, leaving audit_log empty — the one mutating admin
// RPC with no forensic trace of who re-fired which job.
// Fix direction: appendAudit like the sibling RPCs (entity "queue", op "replay",
// record_id = job id, actor from adminActorID).

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// replayAuditUser is the stand-in admin: role for the gate, id for the audit actor.
type replayAuditUser struct{}

func (replayAuditUser) GetID() string      { return "aud-admin-1" }
func (replayAuditUser) GetEmail() string   { return "aud-admin-1@example.com" }
func (replayAuditUser) GetRoles() []string { return []string{"admin"} }

func TestAdminRedQueueReplayAudits(t *testing.T) {
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("ensure audit table: %v", err)
	}
	q := &replayRecordingQueue{}
	// Non-browser POST (no Sec-Fetch-Site/Origin): the CSRF gate passes for
	// the authorized admin, exactly like every sibling RPC test.
	h := asUser(mountAdminBare(t, Config{DB: db, Queue: q}), replayAuditUser{})

	const jobID = "job-911"
	rr := postForm(h, "/admin/queue/_replay/"+jobID, url.Values{})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("setup broken: authorized replay returned %d (body=%s), want 303", rr.Code, rr.Body.String())
	}
	if len(q.replayed) != 1 || q.replayed[0] != jobID {
		t.Fatalf("setup broken: replay not applied exactly once (%v)", q.replayed)
	}

	// Any audit row naming the replay or the job, carrying the actor.
	rows, err := db.Query(`SELECT entity, op, record_id, COALESCE(actor_id,''), COALESCE(diff,'') FROM audit_log`)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()
	naming := 0
	for rows.Next() {
		var entity, op, recordID, actor, diff string
		if err := rows.Scan(&entity, &op, &recordID, &actor, &diff); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		namesReplay := recordID == jobID || strings.Contains(diff, jobID) ||
			strings.Contains(strings.ToLower(op), "replay") ||
			strings.Contains(strings.ToLower(entity), "queue")
		if namesReplay && actor != "" {
			naming++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	if naming < 1 {
		t.Errorf("SECURITY: [queue-replay-audit-gap] POST /admin/queue/_replay/%s replayed the dead-lettered job (303, Replay called) but audit_log carries no row naming the actor and the job — every other mutating admin RPC (grant/revoke/assign incl. their refused branches, module lifecycle, entity writes) appends an audit row, and the handler's own doc calls an ungated replay a privilege-escalation / job-amplification vector; an operator investigating a re-fired job has no forensic trace of who fired it", jobID)
	}
}
