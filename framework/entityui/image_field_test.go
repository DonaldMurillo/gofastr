package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func photosUI(t *testing.T) *testUI {
	return newTestUI(t,
		map[string]entity.EntityConfig{"products": {Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "photo", Type: schema.Image},
		), Exposure: &entity.ExposureConfig{Public: true}}},
		map[string][]map[string]any{"products": {
			{"id": "p1", "name": "Drill", "photo": "/img/drill.png"},
			{"id": "p2", "name": "Saw", "photo": "javascript:alert(1)"},
		}},
		withAPI(map[string]string{"products": "/api/products"}),
	)
}

// An Image field draws a thumbnail, never its URL as text.
func TestImageFieldDrawsAThumbnail(t *testing.T) {
	x := photosUI(t)
	list := listHTML(t, x.ui.List("products"), x.ctx("/products", ""))
	if !strings.Contains(list, `data-cui-comp="ui-thumbnail"`) || !strings.Contains(list, `src="/img/drill.png"`) {
		t.Fatalf("the list did not draw the photo:\n%s", list)
	}
	if strings.Contains(list, ">/img/drill.png<") {
		t.Error("the list printed the photo URL as text")
	}
	record := string(x.ui.Record("products", "p1").RenderCtx(x.ctx("/products/p1", "")))
	if !strings.Contains(record, "fui-thumbnail--lg") || !strings.Contains(record, `name="photo"`) {
		t.Errorf("the record lacks the preview or the field:\n%s", record)
	}
}

// The create form starts at each field's Default; a prefill wins.
func TestCreateStartsAtDefaults(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"products": {Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "category", Type: schema.Enum, Values: []string{"tools", "parts", "spares"}, Default: "parts"},
			schema.Field{Name: "qty", Type: schema.Int, Default: 3},
		), Exposure: &entity.ExposureConfig{Public: true}}},
		nil,
		withAPI(map[string]string{"products": "/api/products"}),
	)
	body := string(x.ui.Create("products").RenderCtx(x.ctx("/products/create", "")))
	if !strings.Contains(body, `selected="" value="parts"`) || !strings.Contains(body, `value="3"`) {
		t.Fatalf("the create form ignored the defaults:\n%s", body)
	}
	body = string(x.ui.Create("products").RenderCtx(x.ctx("/products/create", "?prefill_category=spares")))
	if !strings.Contains(body, `selected="" value="spares"`) {
		t.Fatalf("a prefill lost to the default:\n%s", body)
	}
}

func TestImageFieldRefusesUnsafeURL(t *testing.T) {
	x := photosUI(t)
	list := listHTML(t, x.ui.List("products"), x.ctx("/products", ""))
	if strings.Contains(list, "javascript:") {
		t.Fatalf("SECURITY: a javascript: photo reached the list:\n%s", list)
	}
	record := string(x.ui.Record("products", "p2").RenderCtx(x.ctx("/products/p2", "")))
	if strings.Contains(record, `src="javascript:`) {
		t.Fatalf("SECURITY: a javascript: photo reached an img src:\n%s", record)
	}
}
