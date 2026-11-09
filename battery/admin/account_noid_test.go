package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// idlessAdmin is a signed-in admin whose user value carries no id.
type idlessAdmin struct{}

func (idlessAdmin) GetRoles() []string { return []string{"admin"} }

// idlessMailAdmin carries an address but no id, so the profile card
// reaches its verified-state lookup.
type idlessMailAdmin struct{ idlessAdmin }

func (idlessMailAdmin) GetEmail() string { return "ada@example.com" }

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

// recordingUsers is a user store that records each id the account page
// asks about its verified address and its password.
type recordingUsers struct {
	*auth.EntityUserStore
	asked []string
}

func (s *recordingUsers) IsEmailVerified(ctx context.Context, id string) (bool, error) {
	s.asked = append(s.asked, "verified:"+id)
	return s.EntityUserStore.IsEmailVerified(ctx, id)
}

func (s *recordingUsers) HasPassword(ctx context.Context, id string) (bool, error) {
	s.asked = append(s.asked, "password:"+id)
	return s.EntityUserStore.HasPassword(ctx, id)
}

// An id-less admin's account page reads no account's verified address
// or password state: the store is never asked about an empty id.
func TestAccountPageNeedsUserID(t *testing.T) {
	var users *recordingUsers
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, func(x *env, c *Config) {
		users = &recordingUsers{EntityUserStore: auth.NewEntityUserStore(x.db, "users")}
		if err := users.EnsureSchema(context.Background()); err != nil {
			t.Fatal(err)
		}
		c.Auth = auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: users})
	})
	if rr := get(x.as(idlessMailAdmin{}), "/admin/account"); rr.Code != http.StatusOK {
		t.Fatalf("an id-less admin's account page = %d, want 200", rr.Code)
	}
	if len(users.asked) != 0 {
		t.Errorf("the account page asked the store about %q", users.asked)
	}
}
