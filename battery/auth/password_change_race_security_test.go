package auth

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// memRaceStore runs a hook once, right after the change handler reads the
// hash it is about to verify.
type memRaceStore struct {
	*userStoreWithPassword
	afterRead func()
}

func (s *memRaceStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	u, h, err := s.userStoreWithPassword.FindByEmail(ctx, email)
	if hook := s.afterRead; hook != nil {
		s.afterRead = nil
		hook()
	}
	return u, h, err
}

type entityRaceStore struct {
	*EntityUserStore
	afterRead func()
}

func (s *entityRaceStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	u, h, err := s.EntityUserStore.FindByEmail(ctx, email)
	if hook := s.afterRead; hook != nil {
		s.afterRead = nil
		hook()
	}
	return u, h, err
}

// raceChange signs userID in, arms reset to run between the change's hash
// read and its write, and posts the change.
func raceChange(t *testing.T, store UserStore, userID string, arm func(reset func())) *http.Response {
	t.Helper()
	mgr := New(AuthConfig{SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: store, DevMode: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	sess, err := mgr.SessionStore().Create(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	resetHash, _ := HashPassword("resetpw123")
	arm(func() {
		if err := store.(PasswordSetter).SetPassword(context.Background(), userID, resetHash); err != nil {
			t.Errorf("setup: reset write: %v", err)
		}
	})
	h := &changeHarness{mgr: mgr, r: r}
	return h.change(sess.Token, map[string]string{"current_password": "oldpw123", "password": "attackerpw1"}).Result()
}

// A password change verified the current hash, spent a bcrypt round on
// the new one, then wrote it keyed on the user id alone. A reset that
// landed in between was overwritten by the attacker's password, and the
// change handed them a fresh session (2026-10-27 A1). The write now
// requires the hash it verified to still be stored.
func TestPasswordChangeLosesToReset(t *testing.T) {
	t.Run("entity store", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		users := NewEntityUserStore(db, "users")
		if err := users.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		hash, _ := HashPassword("oldpw123")
		u, err := users.CreateUser(context.Background(), "a@example.com", hash, []string{"user"})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		racer := &entityRaceStore{EntityUserStore: users}
		resp := raceChange(t, racer, u.GetID(), func(reset func()) { racer.afterRead = reset })
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("SECURITY: a password change overtaken by a reset = %d, want 422 like a wrong current password", resp.StatusCode)
		}
		_, stored, err := users.FindByEmail(context.Background(), "a@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if !CheckPassword("resetpw123", stored) {
			t.Error("SECURITY: a stale password change overwrote the completed reset")
		}
	})
	t.Run("store without a swap", func(t *testing.T) {
		mem := newUserStoreWithPassword()
		hash, _ := HashPassword("oldpw123")
		user := &BasicUser{ID: "u-1", Email: "a@example.com", Roles: []string{"user"}}
		mem.users["a@example.com"] = &storeEntry{user: user, hash: hash}
		mem.byID[user.ID] = mem.users["a@example.com"]
		racer := &memRaceStore{userStoreWithPassword: mem}
		resp := raceChange(t, racer, user.ID, func(reset func()) { racer.afterRead = reset })
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("SECURITY: a password change overtaken by a reset = %d, want 422 like a wrong current password", resp.StatusCode)
		}
		if !CheckPassword("resetpw123", mem.users["a@example.com"].hash) {
			t.Error("SECURITY: a stale password change overwrote the completed reset")
		}
	})
}

// SwapPassword's predicate holds on Postgres too.
func TestSwapPasswordPostgres(t *testing.T) {
	db := openPGForBattery(t)
	users := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	u, err := users.CreateUser(ctx, "swap@example.com", "h0", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if ok, err := users.SwapPassword(ctx, u.GetID(), "stale", "h2"); err != nil || ok {
		t.Errorf("SECURITY: swap from a stale hash = %v, %v; want false", ok, err)
	}
	if ok, err := users.SwapPassword(ctx, u.GetID(), "h0", "h1"); err != nil || !ok {
		t.Errorf("swap from the stored hash = %v, %v; want true", ok, err)
	}
	if _, stored, _ := users.FindByEmail(ctx, "swap@example.com"); stored != "h1" {
		t.Errorf("stored hash = %q, want h1", stored)
	}
}
