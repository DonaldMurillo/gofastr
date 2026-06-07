package admin

// Tests for relation (BelongsTo) fields in the entity form: instead of a raw
// text box where you'd type a foreign-key UUID, the admin renders a <select>
// of the related records (Payload/Strapi-style relationship picker).

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func usersConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)
}

// postsWithAuthor adds a BelongsTo(author → users) on the author_id column.
func postsWithAuthorConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false)
}

func TestEntity_FormRendersRelationSelect(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{
		"users": usersConfig(),
		"posts": postsWithAuthorConfig(),
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"users", "posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/users/_create", url.Values{"name": {"Ada Lovelace"}})
	uid := firstID(t, db, "users")

	body := get(h, "/admin/e/posts/new").Body.String()

	// The FK column must render as a <select> of related users, not a raw text
	// box. We assert the related record appears as a selectable option.
	if !strings.Contains(body, `name="author_id"`) {
		t.Fatalf("form missing author_id control; got %q", body)
	}
	if !strings.Contains(body, `value="`+uid+`"`) || !strings.Contains(body, "Ada Lovelace") {
		t.Fatalf("relation field should render the related user as an <option> (value=%s, label Ada Lovelace); got %q", uid, body)
	}
	// Guard against the old behaviour: a bare <input type="text" name="author_id">.
	if strings.Contains(body, `type="text" name="author_id"`) || strings.Contains(body, `name="author_id" type="text"`) {
		t.Fatalf("relation field still rendered as a raw text input")
	}
}

func TestEntity_EditPrefillsRelationSelect(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{
		"users": usersConfig(),
		"posts": postsWithAuthorConfig(),
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"users", "posts"}}, testUser{"u1"})

	postForm(h, "/admin/e/users/_create", url.Values{"name": {"Grace Hopper"}})
	uid := firstID(t, db, "users")
	postForm(h, "/admin/e/posts/_create", url.Values{"title": {"P"}, "author_id": {uid}})
	pid := firstID(t, db, "posts")

	body := get(h, "/admin/e/posts/edit/"+pid).Body.String()
	// The current author must be the selected option.
	if !strings.Contains(body, "Grace Hopper") {
		t.Fatalf("edit form should list the related user; got %q", body)
	}
	if !rxSelectedOption(body, uid) {
		t.Fatalf("edit form should pre-select the current author (%s); got %q", uid, body)
	}
}

// rxSelectedOption reports whether body has an <option ... value="id" ... selected>.
func rxSelectedOption(body, id string) bool {
	for _, frag := range strings.Split(body, "<option") {
		if strings.Contains(frag, `value="`+id+`"`) && strings.Contains(frag, "selected") {
			return true
		}
	}
	return false
}

var _ = http.MethodGet
