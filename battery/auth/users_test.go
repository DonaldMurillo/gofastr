package auth

import (
	"context"
	"errors"
	"testing"
)

// TestListUsersUnsupportedStore pins that a store which does not
// implement UserLister yields ErrListUsersUnsupported — never a silent
// empty page. memoryUserStore only implements UserStore.
func TestListUsersUnsupportedStore(t *testing.T) {
	store := newMemoryUserStore() // UserStore only, no UserLister
	mgr := New(AuthConfig{
		JWTSecret: "test-secret",
		UserStore: store,
		DevMode:   true,
	})

	users, total, err := mgr.ListUsers(context.Background(), ListUsersOptions{Limit: 10})
	if !errors.Is(err, ErrListUsersUnsupported) {
		t.Fatalf("expected ErrListUsersUnsupported, got err=%v users=%v total=%d", err, users, total)
	}
	if users != nil || total != 0 {
		t.Fatalf("unsupported store must return nil/0, got users=%v total=%d", users, total)
	}
}

// TestListUsersClampsOptions pins the AuthManager.ListUsers clamping
// contract (Limit<=0→50, >500→500, Offset<0→0) using a recording
// UserLister that echoes back the opts it received.
func TestListUsersClampsOptions(t *testing.T) {
	cases := []struct {
		name      string
		in        ListUsersOptions
		wantLimit int
		wantOff   int
	}{
		{"zero limit → 50", ListUsersOptions{Limit: 0, Offset: 0}, 50, 0},
		{"negative limit → 50", ListUsersOptions{Limit: -1, Offset: 5}, 50, 5},
		{"over max → 500", ListUsersOptions{Limit: 9999, Offset: 0}, 500, 0},
		{"negative offset → 0", ListUsersOptions{Limit: 20, Offset: -3}, 20, 0},
		{"in-range passthrough", ListUsersOptions{Limit: 7, Offset: 14}, 7, 14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingLister{}
			mgr := New(AuthConfig{
				JWTSecret: "test-secret",
				UserStore: rec,
				DevMode:   true,
			})
			if _, _, err := mgr.ListUsers(context.Background(), tc.in); err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if rec.got.Limit != tc.wantLimit {
				t.Errorf("Limit: got %d, want %d", rec.got.Limit, tc.wantLimit)
			}
			if rec.got.Offset != tc.wantOff {
				t.Errorf("Offset: got %d, want %d", rec.got.Offset, tc.wantOff)
			}
		})
	}
}

// recordingLister is a UserStore + UserLister that captures the opts
// AuthManager.ListUsers forwarded (after clamping).
type recordingLister struct {
	got ListUsersOptions
}

func (r *recordingLister) FindByEmail(_ context.Context, _ string) (User, string, error) {
	return nil, "", ErrUserNotFound
}
func (r *recordingLister) FindByID(_ context.Context, _ string) (User, error) {
	return nil, ErrUserNotFound
}
func (r *recordingLister) CreateUser(_ context.Context, _ string, _ string, _ []string) (User, error) {
	return nil, ErrEmailTaken
}
func (r *recordingLister) ListUsers(_ context.Context, opts ListUsersOptions) ([]User, int, error) {
	r.got = opts
	return nil, 0, nil
}
