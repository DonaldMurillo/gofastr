package sdkdocs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestFieldsTableMarksNoQueryField(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "number", Type: schema.String, NoQuery: true},
	}}
	html := (&entityScreen{}).fieldsTable(cfg).String()
	if !strings.Contains(html, "number") || !strings.Contains(html, "not filterable/sortable") {
		t.Fatalf("NoQuery field note missing from SDK docs table: %s", html)
	}
}

func TestExampleFilterFieldSkipsNoQueryFields(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "number", Type: schema.Int, NoQuery: true},
		{Name: "label", Type: schema.String},
	}}
	if got := exampleFilterField(cfg); got != "label" {
		t.Fatalf("exampleFilterField = %q, want queryable label", got)
	}
}

func TestExampleFilterFieldRejectsNoQueryCreatedAtFallback(t *testing.T) {
	cfg := entity.EntityConfig{Fields: []schema.Field{
		{Name: "created_at", Type: schema.Timestamp, NoQuery: true, AutoGenerate: schema.AutoTimestamp},
	}}
	if got := exampleFilterField(cfg); got != "id" {
		t.Fatalf("exampleFilterField = %q, want id fallback", got)
	}
}
