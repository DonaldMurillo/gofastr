package admin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// accountEnv is an admin with an auth store holding one admin user,
// returned signed in.
func accountEnv(t *testing.T) (*env, *auth.EntityUserStore, auth.User) {
	t.Helper()
	var users *auth.EntityUserStore
	var u auth.User
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, func(x *env, c *Config) {
		ctx := context.Background()
		users = auth.NewEntityUserStore(x.db, "users")
		if err := users.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		hash, err := auth.HashPassword("oldpw123")
		if err != nil {
			t.Fatal(err)
		}
		u, err = users.CreateUser(ctx, "ada@example.com", hash, []string{"admin", "billing"})
		if err != nil {
			t.Fatal(err)
		}
		c.Auth = auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: users})
	})
	return x, users, u
}

// The account page names the caller, their address's verified state
// and their roles, offers the theme choice, and changes the password
// through the auth route with only the route's own fields.
func TestAccountPageShowsProfile(t *testing.T) {
	x, _, u := accountEnv(t)
	rr := get(x.as(u), "/admin/account")
	if rr.Code != http.StatusOK {
		t.Fatalf("account page: %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Account settings", "ada@example.com", "Unverified", ">admin<", ">billing<", "data-hui-theme-toggle"} {
		if !strings.Contains(body, want) {
			t.Errorf("the account page lacks %q", want)
		}
	}
	form := regexp.MustCompile(`(?s)<form[^>]*id="admin-password-form".*?</form>`).FindString(body)
	if form == "" {
		t.Fatal("no password form")
	}
	if !strings.Contains(form, `data-cui-rpc="/auth/password"`) {
		t.Errorf("the password form does not post to the auth route: %s", form)
	}
	names := regexp.MustCompile(`<input[^>]*\bname="([^"]+)"`).FindAllStringSubmatch(form, -1)
	var got []string
	for _, n := range names {
		got = append(got, n[1])
	}
	if strings.Join(got, ",") != "current_password,password,confirm_password" {
		t.Errorf("the form sends %v; the route refuses any field but its own", got)
	}
	for _, ac := range []string{`autocomplete="current-password"`, `autocomplete="new-password"`} {
		if !strings.Contains(form, ac) {
			t.Errorf("the form lacks %s", ac)
		}
	}
}

// An account that signs in without a password is told how to set one,
// not shown a form whose current-password check can never pass.
func TestAccountPageNoPasswordAccount(t *testing.T) {
	x, users, u := accountEnv(t)
	if err := users.ClearPassword(context.Background(), u.GetID()); err != nil {
		t.Fatal(err)
	}
	body := get(x.as(u), "/admin/account").Body.String()
	if strings.Contains(body, `id="admin-password-form"`) {
		t.Error("a passwordless account got a change-password form")
	}
	if !strings.Contains(body, "signs in without a password") {
		t.Error("a passwordless account is not told how to set one")
	}
}

// Without Auth the page has no password section; the shell links the
// page from the account menu and names it in the trail.
func TestAccountPageWithoutAuth(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	body := get(x.as(theAdmin), "/admin/account").Body.String()
	if !strings.Contains(body, "Account settings") || strings.Contains(body, `id="admin-password-form"`) {
		t.Errorf("without Auth: want the page and no password form")
	}
	if !strings.Contains(get(x.as(theAdmin), "/admin").Body.String(), `href="/admin/account"`) {
		t.Error("the account menu does not link the account page")
	}
	if rr := get(x.as(nil), "/admin/account"); rr.Code == http.StatusOK {
		t.Errorf("SECURITY: an anonymous caller read the account page: %d", rr.Code)
	}
}

// The account page sets the caller's display name through the store's
// NameStore, and the shell then names them by it: the account menu's
// "Signed in as" and the avatar's two initials.
func TestAccountNameNamesTheShell(t *testing.T) {
	x, users, u := accountEnv(t)
	ctx := context.Background()
	other, err := users.CreateUser(ctx, "grace@example.com", "hash", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	h := x.as(u)
	form := regexp.MustCompile(`(?s)<form[^>]*id="admin-name-form".*?</form>`).FindString(get(h, "/admin/account").Body.String())
	if !strings.Contains(form, `data-cui-rpc="/admin/account/_name"`) || !strings.Contains(form, `name="name"`) {
		t.Fatalf("no name form posting to the account route: %s", form)
	}
	// A user_id rides along: the route names the caller, never it.
	rr := post(h, "/admin/account/_name", url.Values{"name": {"  Ada Lovelace "}, "user_id": {other.GetID()}})
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "result=name-saved") {
		t.Fatalf("save name: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if name, _ := users.UserName(ctx, u.GetID()); name != "Ada Lovelace" {
		t.Errorf("the caller's name is %q", name)
	}
	if name, _ := users.UserName(ctx, other.GetID()); name != "" {
		t.Errorf("SECURITY: the post named another user %q", name)
	}
	shell := get(h, "/admin").Body.String()
	if !strings.Contains(shell, "Signed in as Ada Lovelace") {
		t.Error("the account menu does not use the name")
	}
	if !strings.Contains(shell, ">AL<") {
		t.Error("the avatar does not draw the name's two initials")
	}
	// A name with a control character is refused and changes nothing.
	rr = post(h, "/admin/account/_name", url.Values{"name": {"Ada\nLovelace"}})
	if !strings.Contains(rr.Header().Get("Location"), "result=name-refused") {
		t.Errorf("a name with a newline: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if name, _ := users.UserName(ctx, u.GetID()); name != "Ada Lovelace" {
		t.Errorf("a refused name changed the stored one to %q", name)
	}
	if rr := post(x.as(nil), "/admin/account/_name", url.Values{"name": {"Mallory"}}); rr.Code == http.StatusSeeOther && strings.Contains(rr.Header().Get("Location"), "name-saved") {
		t.Error("SECURITY: an anonymous caller saved a name")
	}
}
