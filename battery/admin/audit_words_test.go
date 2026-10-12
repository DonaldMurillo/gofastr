package admin

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A Roles row names the role and the permission it was granted, revoked
// or refused; a User roles row names the account by email and the roles
// it was given or the one refused. A row with no detail keeps the table
// name and id.
func TestAuditNamesAccessChanges(t *testing.T) {
	x, userID := activityEnv(t)
	now := time.Now().UTC()
	rows := []struct{ op, record, diff string }{
		{"grant", "billing", `{"permission":"plans:write"}`},
		{"revoke", "support", `{"permission":"queue:read"}`},
		{"grant-refused", "ops", `{"permission":"users:delete"}`},
		{"revoke-refused", "ops", `{"permission":"users:read"}`},
		{"assign-roles", userID, `{"roles":["billing","support"]}`},
		{"assign-roles-refused", userID, `{"roles":["admin"],"refused_role":"admin"}`},
		{"assign-roles", "u-gone", `{"roles":[]}`},
		{"grant", "nodetail", ``},
		{"grant", "noperm", `{}`},
	}
	for i, r := range rows {
		x.seedAuditDiff("ac"+strconv.Itoa(i), "", "access", r.op, r.record, userID, now.Add(-time.Duration(i)*time.Minute), r.diff)
	}
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	for _, want := range []string{
		"Role · </span>billing",
		"User · </span>ada@example.com",
		">billing</span>",
		"Refused to assign <code",
		"User · </span>u-gone",
		"No roles",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the audit page lacks %q:\n%s", want, body)
		}
	}
	for _, want := range []string{
		`Granted <code[^>]*>plans:write</code>`,
		`Revoked <code[^>]*>queue:read</code>`,
		`Refused to grant <code[^>]*>users:delete</code>`,
		`Refused to revoke <code[^>]*>users:read</code>`,
		`Refused to assign <code[^>]*>admin</code>`,
		`access <code[^>]*>nodetail</code>`,
		`access <code[^>]*>noperm</code>`,
	} {
		if !regexp.MustCompile(want).MatchString(body) {
			t.Errorf("the audit page does not match %q", want)
		}
	}
}

// A bulk run's row names how many records it took and what it did, with
// what it skipped or failed on; an app's own action and a detail that
// does not parse keep the run id.
func TestAuditNamesBulkRuns(t *testing.T) {
	x, userID := activityEnv(t)
	now := time.Now().UTC()
	diffs := []string{
		`{"action":"delete","count":2,"done":2,"skipped":0,"failed":0,"status":"done"}`,
		`{"action":"restore","count":3,"done":1,"skipped":2,"failed":0,"status":"done"}`,
		`{"action":"set:status:published","count":1,"done":0,"skipped":0,"failed":1,"status":"done"}`,
		`{"action":"archive","count":2,"done":2,"skipped":0,"failed":0,"status":"done"}`,
		`not json`,
	}
	for i, d := range diffs {
		id := "bk" + strconv.Itoa(i)
		x.seedAuditDiff(id, "", "posts", "bulk", "run-"+id, userID, now.Add(-time.Duration(i)*time.Minute), d)
	}
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	for _, want := range []string{
		">2 posts<", "Deleted",
		">3 posts<", "Restored", "2 skipped",
		">1 post<", "Updated", "1 failed",
		"run-bk3", "run-bk4",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the audit page lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "fui-data-table--responsive-cards") {
		t.Error("the audit table scrolls on a phone, hiding the record and its changes")
	}
	for _, gone := range []string{"run-bk0", "run-bk1", "run-bk2"} {
		if strings.Contains(body, gone) {
			t.Errorf("a named bulk row still shows its run id %s", gone)
		}
	}
}
