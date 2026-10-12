package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// An entity registered after EntityUI gets the checks EntityUI ran on the
// ones before it: an input naming no kind, or a view with no where and no
// filter, panics at App.Entity and names the entity, in a group too.
func TestEntityUILateEntityIsChecked(t *testing.T) {
	memo := func(d *entity.DisplayConfig) entity.EntityConfig {
		return entity.EntityConfig{
			Table:    "memos",
			Fields:   []schema.Field{{Name: "title", Type: schema.String}, {Name: "done", Type: schema.Bool}},
			Exposure: &entity.ExposureConfig{Public: true},
			Display:  d,
		}.WithTimestamps(false)
	}
	bad := map[string]*entity.DisplayConfig{
		"unknown kind":    {Fields: map[string]entity.FieldDisplay{"title": {Input: "typo"}}},
		"unfiltered view": {Views: []entity.ListView{{Key: "open"}}},
	}
	for name, d := range bad {
		for _, grouped := range []bool{false, true} {
			app := entityUIAuditApp(t, memDB(t), "notes", false)
			app.EntityUI(entityui.Extensions{})
			msg := panicText(t, func() {
				if grouped {
					app.GroupEntity(app.Group("/v2"), "memos", memo(d))
					return
				}
				app.Entity("memos", memo(d))
			})
			if !strings.Contains(msg, `"memos"`) {
				t.Errorf("%s (grouped=%v): panic = %q, want one naming memos", name, grouped, msg)
			}
			if _, err := app.Registry.Get("memos"); err == nil {
				t.Errorf("%s (grouped=%v): the refused entity was registered", name, grouped)
			}
		}
	}
	app := entityUIAuditApp(t, memDB(t), "notes", false)
	app.EntityUI(entityui.Extensions{})
	app.Entity("memos", memo(&entity.DisplayConfig{
		Fields: map[string]entity.FieldDisplay{"title": {Input: "markdown"}},
		Views:  []entity.ListView{{Key: "open", Where: "done = false"}},
	}))
}
