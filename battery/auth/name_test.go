package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The entity store keeps a display name: EnsureSchema adds the column to
// a table made before it existed, a new user's name is "", SetUserName
// stores a cleaned name and UserName reads it back.
func TestEntityUserStoreName(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	// Twice: the column is added once.
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema again: %v", err)
	}
	u, err := store.CreateUser(ctx, "ada@example.com", "hash", []string{"user"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var _ NameStore = store
	if name, err := store.UserName(ctx, u.GetID()); err != nil || name != "" {
		t.Fatalf("a new user's name is %q, %v; want empty", name, err)
	}
	if err := store.SetUserName(ctx, u.GetID(), "  Ada Lovelace  "); err != nil {
		t.Fatalf("set name: %v", err)
	}
	if name, err := store.UserName(ctx, u.GetID()); err != nil || name != "Ada Lovelace" {
		t.Fatalf("name is %q, %v; want the trimmed name", name, err)
	}
	if err := store.SetUserName(ctx, u.GetID(), ""); err != nil {
		t.Fatalf("clear name: %v", err)
	}
	if name, _ := store.UserName(ctx, u.GetID()); name != "" {
		t.Fatalf("a cleared name reads %q", name)
	}
	if _, err := store.UserName(ctx, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("an unknown user's name: %v, want ErrUserNotFound", err)
	}
	if err := store.SetUserName(ctx, "nobody", "X"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("naming an unknown user: %v, want ErrUserNotFound", err)
	}
}

// A display name is drawn in other people's screens, so it carries no
// control characters, is valid UTF-8 and has a length cap. The store
// refuses what CleanName refuses.
func TestCleanNameRefusesUnsafeNames(t *testing.T) {
	for _, bad := range []string{
		"Ada\nLovelace",
		"Ada\x1b[31m",
		"Ada‮ecalevoL",
		"\xff\xfe",
		strings.Repeat("a", MaxNameRunes+1),
	} {
		if _, err := CleanName(bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CleanName(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	if got, err := CleanName(strings.Repeat("é", MaxNameRunes)); err != nil || got == "" {
		t.Errorf("a name at the cap is refused: %v", err)
	}
	db := setupTestDB(t)
	defer db.Close()
	store := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	u, _ := store.CreateUser(ctx, "ada@example.com", "hash", nil)
	if err := store.SetUserName(ctx, u.GetID(), "Ada\nLovelace"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("the store stored a name with a newline: %v", err)
	}
}
