package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestAccountNameChangeIsAudited: the admin's package contract is an
// audit row for every mutation, and the name change wrote none.
func TestAccountNameChangeIsAudited(t *testing.T) {
	x, _, u := accountEnv(t)
	rr := post(x.as(u), "/admin/account/_name", url.Values{"name": {"  Ada Lovelace "}})
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "result=name-saved") {
		t.Fatalf("save name: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	var actor, record, diff string
	if err := x.db.QueryRow(`SELECT actor_id, record_id, diff FROM audit_log WHERE entity = 'account' AND op = 'set-name'`).Scan(&actor, &record, &diff); err != nil {
		t.Fatalf("no audit row for the name change: %v", err)
	}
	if actor != u.GetID() || record != u.GetID() || !strings.Contains(diff, `"Ada Lovelace"`) {
		t.Errorf("audit row actor=%q record=%q diff=%s", actor, record, diff)
	}

	// A refused name changes nothing and so records nothing.
	post(x.as(u), "/admin/account/_name", url.Values{"name": {"Ada\nLovelace"}})
	if ops := x.auditOps("account"); len(ops) != 1 {
		t.Errorf("audit ops after a refused name = %v, want the one set-name", ops)
	}
}
