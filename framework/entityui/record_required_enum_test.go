package entityui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A required enum holding a value the field no longer lists matched no
// option: the segmented control submitted nothing and the select
// submitted its first option, rewriting the stored value on an
// untouched save. Both now draw a select whose disabled placeholder is
// chosen, so the save stops until a listed value is picked.
func TestRequiredEnumKeepsStaleValue(t *testing.T) {
	cfg := entity.EntityConfig{
		Table: "tasks",
		Fields: []schema.Field{
			{Name: "status", Type: schema.Enum, Required: true, Values: []string{"a", "b", "c"}},
			{Name: "level", Type: schema.Enum, Required: true, Values: []string{"l1", "l2", "l3", "l4", "l5"}},
		},
	}
	x := newTestUI(t, map[string]entity.EntityConfig{"tasks": cfg.WithTimestamps(false)},
		map[string][]map[string]any{"tasks": {{"id": "t1", "status": "old", "level": "old"}}},
		withAPI(map[string]string{"tasks": "/api/tasks"}))
	body := string(x.ui.Record("tasks", "t1").Base("/tasks").RenderCtx(x.userCtx("/tasks/t1", "", "u1")))
	for _, name := range []string{"status", "level"} {
		sel := regexp.MustCompile(`(?s)<select[^>]*name="` + name + `"[^>]*>.*?</select>`).FindString(body)
		if sel == "" {
			t.Errorf("%s: no select drawn", name)
			continue
		}
		opts := regexp.MustCompile(`<option[^>]*>`).FindAllString(sel, -1)
		if len(opts) == 0 || !strings.Contains(opts[0], "disabled") || !strings.Contains(opts[0], `value=""`) {
			t.Errorf("%s: first option = %v, want a disabled empty placeholder", name, opts)
		}
		for _, o := range opts[1:] {
			if strings.Contains(o, "selected") {
				t.Errorf("%s: a listed option is selected: %s", name, o)
			}
		}
	}
}
