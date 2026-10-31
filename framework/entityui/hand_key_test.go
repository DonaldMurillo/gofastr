package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// plansUI keys plans on a code the caller assigns: required, with no
// generator and no default, so nothing but the create form can set it.
func plansUI(t *testing.T, required bool) *testUI {
	t.Helper()
	installOwnerExtractor(t)
	return newTestUI(t,
		map[string]entity.EntityConfig{"plans": {
			PrimaryKey: []string{"code"},
			Fields: fields(
				schema.Field{Name: "code", Type: schema.String, Required: required},
				schema.Field{Name: "title", Type: schema.String},
			),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"plans": {{"code": "basic", "title": "Basic"}}},
		withAPI(map[string]string{"plans": "/api/plans"}),
		withSingleConn(),
	)
}

// A required one-column key with no generator is set on create, locked
// after, and blanked on a duplicate, as a composite key's columns are.
func TestHandKeySetOnCreate(t *testing.T) {
	x := plansUI(t, true)
	create := string(x.ui.Create("plans").Base("/plans").
		RenderCtx(x.userCtx("/plans/create", "", "u1")))
	if !strings.Contains(create, `name="code"`) {
		t.Fatalf("the create form has no input for the required key code:\n%s", create)
	}
	edit := string(x.ui.Record("plans", "basic").Base("/plans").
		RenderCtx(x.userCtx("/plans/basic", "", "u1")))
	if !strings.Contains(edit, `value="Basic"`) {
		t.Fatalf("the edit form did not read the record:\n%s", edit)
	}
	if strings.Contains(edit, `name="code"`) {
		t.Fatalf("the edit form submits the key code:\n%s", edit)
	}
	dup := string(x.ui.Create("plans").Base("/plans").
		RenderCtx(x.userCtx("/plans/create", "?duplicate=basic", "u1")))
	if !strings.Contains(dup, `value="Basic"`) || strings.Contains(dup, `value="basic"`) {
		t.Fatalf("the duplicate must copy the title and blank the key:\n%s", dup)
	}
}

// An optional key stays off the create form: a hook may fill it, and a
// blank input would post "" over the absence the hook looks for.
func TestOptionalHandKeyStaysOff(t *testing.T) {
	x := plansUI(t, false)
	create := string(x.ui.Create("plans").Base("/plans").
		RenderCtx(x.userCtx("/plans/create", "", "u1")))
	if !strings.Contains(create, `name="title"`) || strings.Contains(create, `name="code"`) {
		t.Fatalf("the create form must carry title and not the optional key:\n%s", create)
	}
}

// A generated key stays the server's even when Required, and so does a
// ReadOnly field: neither draws on the create form.
func TestServerOwnedFieldsStayOff(t *testing.T) {
	installOwnerExtractor(t)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"tickets": {
			PrimaryKey: []string{"ref"},
			Fields: fields(
				schema.Field{Name: "ref", Type: schema.UUID, Required: true, AutoGenerate: schema.AutoUUID},
				schema.Field{Name: "score", Type: schema.Int, ReadOnly: true},
				schema.Field{Name: "title", Type: schema.String},
			),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"tickets": {}},
		withAPI(map[string]string{"tickets": "/api/tickets"}),
		withSingleConn(),
	)
	create := string(x.ui.Create("tickets").Base("/tickets").
		RenderCtx(x.userCtx("/tickets/create", "", "u1")))
	if !strings.Contains(create, `name="title"`) {
		t.Fatalf("the create form has no title input:\n%s", create)
	}
	for _, refuse := range []string{`name="ref"`, `name="score"`} {
		if strings.Contains(create, refuse) {
			t.Fatalf("the create form carries the server-owned %s:\n%s", refuse, create)
		}
	}
}
