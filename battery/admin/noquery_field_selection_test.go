package admin

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestNoQueryLabelIsNotSearchField(t *testing.T) {
	ent := entity.Define("contacts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, NoQuery: true},
			{Name: "description", Type: schema.Text},
		},
	})
	if got := labelField(ent); got != "name" {
		t.Fatalf("labelField = %q, want NoQuery display label name", got)
	}
	if got := searchField(ent); got != "description" {
		t.Fatalf("searchField = %q, want queryable description", got)
	}
}

func TestNoQueryColumnRendersButIsNotSortable(t *testing.T) {
	ent := entity.Define("contacts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, NoQuery: true},
			{Name: "description", Type: schema.Text},
		},
	}.WithTimestamps(false))

	if !containsColumn(listColumns(ent), "name") {
		t.Fatalf("NoQuery display column missing from listColumns: %v", listColumns(ent))
	}
	if containsColumn(sortableColumns(ent), "name") {
		t.Fatalf("NoQuery column remained sortable: %v", sortableColumns(ent))
	}
	if !containsColumn(sortableColumns(ent), "description") {
		t.Fatalf("queryable column was not sortable: %v", sortableColumns(ent))
	}
}

func containsColumn(columns []string, want string) bool {
	for _, column := range columns {
		if column == want {
			return true
		}
	}
	return false
}
