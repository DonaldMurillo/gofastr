package sdkdocs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func TestFieldsTableMarksNoQueryField(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "number", Type: schema.String, NoQuery: true},
	}}
	html := (&entityScreen{}).fieldsTable(cfg, "").String()
	if !strings.Contains(html, "number") || !strings.Contains(html, "not filterable/sortable") {
		t.Fatalf("NoQuery field note missing from SDK docs table: %s", html)
	}
}

func TestExampleFilterFieldSkipsNoQueryFields(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "number", Type: schema.Int, NoQuery: true},
		{Name: "label", Type: schema.String},
	}}
	sample := exampleFilter(cfg)
	if sample.field != "label" {
		t.Fatalf("exampleFilter field = %q, want queryable label", sample.field)
	}
	if sample.op != filter.OpGte {
		t.Fatalf("exampleFilter op = %q, want _gte on a String column", sample.op)
	}
}

func TestExampleFilterFieldRejectsNoQueryCreatedAtFallback(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
		{Name: "created_at", Type: schema.Timestamp, NoQuery: true, AutoGenerate: schema.AutoTimestamp},
	}}
	sample := exampleFilter(cfg)
	if sample.field != "id" || sample.op != filter.OpEq {
		t.Fatalf("exampleFilter = %+v, want id with plain equality", sample)
	}
}
