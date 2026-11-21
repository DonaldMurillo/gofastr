package entityui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestRecordBoolTextMatchesList(t *testing.T) {
	config := entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}, {Name: "active", Type: schema.Bool}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"flags": config}, nil)
	ctx := fixture.ctx("/flags", "")
	metadata, err := fixture.ui.meta("flags")
	if err != nil {
		t.Fatal(err)
	}
	field := schema.Field{Type: schema.Bool, Name: "active"}
	fb := &formBuilder{m: metadata}
	for _, value := range []any{nil, "", false, true, "0", "1", int64(0), int64(1)} {
		row := map[string]any{"active": value}
		list := fixture.ui.plainText(ctx, &listState{m: metadata}, labelResolver{}, field, row, "active")
		record := string(fb.display(ctx, field, row))
		if list == "" {
			list = string(muted())
		}
		if record != list {
			t.Errorf("record text for %#v = %q, list says %q", value, record, list)
		}
	}
}
