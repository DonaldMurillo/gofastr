package auth

import (
	"context"
	"errors"
	"testing"
)

type roleWriteCounter struct {
	UserStore
	writes int
}

func (store *roleWriteCounter) UpdateRoles(context.Context, string, []string) error {
	store.writes++
	return nil
}

func TestRoleIDsRefuseUnstorableText(t *testing.T) {
	database := setupTestDB(t)
	t.Cleanup(func() { database.Close() })
	store := NewEntityUserStore(database, "users")
	for _, invalid := range []string{"id\x00tail", "id\xff", "id\xe2\x82"} {
		if _, err := database.Exec(`INSERT INTO users (id, email, password_hash, roles) VALUES (?, ?, '', '["user"]')`, invalid, invalid+"@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.FindByID(t.Context(), invalid); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("FindByID accepted unstorable %q: %v", invalid, err)
		}
		if err := store.UpdateRoles(t.Context(), invalid, []string{"admin"}); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("UpdateRoles accepted unstorable %q: %v", invalid, err)
		}
		var roles string
		if err := database.QueryRow(`SELECT roles FROM users WHERE id = ?`, invalid).Scan(&roles); err != nil || roles != `["user"]` {
			t.Errorf("unstorable ID changed roles to %q: %v", roles, err)
		}
	}
	counter := &roleWriteCounter{UserStore: newMemoryUserStore()}
	manager := New(AuthConfig{UserStore: counter})
	for _, invalid := range []string{"id\x00tail", "id\xff", "id\xe2\x82"} {
		if err := manager.SetUserRoles(t.Context(), invalid, []string{"admin"}); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("manager accepted unstorable %q: %v", invalid, err)
		}
	}
	if counter.writes != 0 {
		t.Errorf("manager forwarded %d unstorable IDs", counter.writes)
	}
	if _, err := database.Exec(`INSERT INTO users (id, email, password_hash) VALUES (' padded ', 'valid@example.com', '')`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRoles(t.Context(), " padded ", []string{"admin"}); err != nil {
		t.Fatalf("exact padded ID refused: %v", err)
	}
	if _, err := store.FindByID(t.Context(), "padded"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("lookup trimmed an ID: %v", err)
	}
}
