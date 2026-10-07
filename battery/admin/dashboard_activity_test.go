package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// activityEnv exposes posts, holds one post and one signed-up user, and
// wires Auth so the admin can name an actor.
func activityEnv(t *testing.T) (*env, string) {
	t.Helper()
	var userID string
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, func(x *env, c *Config) {
		ctx := context.Background()
		users := auth.NewEntityUserStore(x.db, "users")
		if err := users.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		u, err := users.CreateUser(ctx, "ada@example.com", "$2a$10$hash", []string{"admin"})
		if err != nil {
			t.Fatal(err)
		}
		userID = u.GetID()
		c.Auth = auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: users})
	})
	x.insert("posts", map[string]any{"id": "p-1", "title": "Hello world", "status": "draft"})
	return x, userID
}

// Each activity line reads as a sentence: who, what they did, to which
// record by its title, and how long ago. A deleted record is named by
// its entity alone, and an actor no account matches keeps its id.
func TestRecentActivityReadsAsSentences(t *testing.T) {
	x, userID := activityEnv(t)
	now := time.Now().UTC()
	x.seedAudit("a1", "", "posts", "update", "p-1", userID, now.Add(-5*time.Minute))
	x.seedAudit("a2", "", "posts", "delete", "p-9", "", now.Add(-2*time.Hour))
	x.seedAudit("a3", "", "billing", "create", "rec-9", "ghost-1", now.Add(-3*24*time.Hour))
	body := get(x.as(theAdmin), "/admin").Body.String()
	for _, want := range []string{
		"ada@example.com updated Hello world", "5m ago",
		"System deleted Post", "2h ago",
		"ghost-1 created billing rec-9", "3d ago",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recent activity lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, userID) {
		t.Errorf("recent activity shows the actor's raw id %q", userID)
	}
}

// The Audit log page names the actor the same way.
func TestAuditPageNamesTheActor(t *testing.T) {
	x, userID := activityEnv(t)
	x.seedAudit("a1", "", "posts", "update", "p-1", userID, time.Now().UTC())
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	if !strings.Contains(body, "ada@example.com") || strings.Contains(body, ">"+userID+"<") {
		t.Errorf("the audit page does not name the actor:\n%s", body)
	}
}
