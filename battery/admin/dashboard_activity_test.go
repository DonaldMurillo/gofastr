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

// Each activity line reads as a sentence: who in bold, what they did,
// the record by its title as a link to it, and how long ago. An account
// reads as its email's local part, the full email on hover. A deleted
// record is named by its entity alone and links nowhere, and an actor no
// account matches keeps its id.
func TestRecentActivityReadsAsSentences(t *testing.T) {
	x, userID := activityEnv(t)
	now := time.Now().UTC()
	x.seedAudit("a1", "", "posts", "update", "p-1", userID, now.Add(-5*time.Minute))
	x.seedAudit("a2", "", "posts", "delete", "p-9", "", now.Add(-2*time.Hour))
	x.seedAudit("a3", "", "billing", "create", "rec-9", "ghost-1", now.Add(-3*24*time.Hour))
	body := get(x.as(theAdmin), "/admin").Body.String()
	if strings.Contains(body, "/admin/entities/posts/p-9") {
		t.Errorf("a deleted record links to its record screen:\n%s", body)
	}
	for _, want := range []string{
		`title="ada@example.com">ada</strong> updated <a`, `href="/admin/entities/posts/p-1"`, `>Hello world</a>`, "5m ago",
		"<strong>System</strong> deleted Post", "2h ago",
		"<strong>ghost-1</strong> created billing rec-9", "3d ago",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recent activity lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, userID) {
		t.Errorf("recent activity shows the actor's raw id %q", userID)
	}
}

// The activity line is markup built from escaped parts: an actor id or
// a record title that holds markup or a placeholder draws as text.
func TestRecentActivityEscapesTitles(t *testing.T) {
	x, userID := activityEnv(t)
	x.insert("posts", map[string]any{"id": "p-2", "title": "<img src=x onerror=alert(1)>", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p-3", "title": "{actor}", "status": "draft"})
	now := time.Now().UTC()
	x.seedAudit("a1", "", "posts", "update", "p-2", userID, now)
	x.seedAudit("a2", "", "posts", "update", "p-3", userID, now)
	x.seedAudit("a3", "", "posts", "update", "p-1", "<i>ghost</i>", now)
	x.seedAudit("a4", "", "billing", "create", "<u>r</u>", "", now)
	body := get(x.as(theAdmin), "/admin").Body.String()
	if strings.Contains(body, "<u>r") || !strings.Contains(body, "created billing &lt;u&gt;r&lt;/u&gt;") {
		t.Errorf("an unlinked record label's markup is not escaped:\n%s", body)
	}
	if strings.Contains(body, "<i>ghost") || !strings.Contains(body, "<strong>&lt;i&gt;ghost&lt;/i&gt;</strong>") {
		t.Errorf("an actor id's markup is not escaped:\n%s", body)
	}
	if strings.Contains(body, "<img src=x") || !strings.Contains(body, ">&lt;img src=x onerror=alert(1)&gt;</a>") {
		t.Errorf("a record title's markup is not escaped:\n%s", body)
	}
	if !strings.Contains(body, `data-cui-comp="ui-link">{actor}</a>`) {
		t.Errorf("a record title's placeholder was filled:\n%s", body)
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
