package admin

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// idlessAdmin is a signed-in admin whose user value carries no id.
type idlessAdmin struct{}

func (idlessAdmin) GetRoles() []string { return []string{"admin"} }

// A caller with no user id has no account to name: the route refuses
// and stores nothing. adminActorID's "unknown" stand-in for audit rows
// once reached the store as a user id, naming whichever row has it.
func TestAccountNameNeedsUserID(t *testing.T) {
	x, users, _ := accountEnv(t)
	// A user whose id is the stand-in: the post named this row.
	if _, err := x.db.Exec(`UPDATE users SET id = 'unknown'`); err != nil {
		t.Fatal(err)
	}
	rr := post(x.as(idlessAdmin{}), "/admin/account/_name", url.Values{"name": {"Mallory"}})
	if strings.Contains(rr.Header().Get("Location"), "name-saved") || !strings.Contains(rr.Header().Get("Location"), "result=failed") {
		t.Errorf("an id-less caller's name post = %d %s, want result=failed", rr.Code, rr.Header().Get("Location"))
	}
	if name, _ := users.UserName(context.Background(), "unknown"); name != "" {
		t.Errorf("the post named user %q: %q", "unknown", name)
	}
}
