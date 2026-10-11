package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// displayRich is a full, valid Display block. Screen hints must never show
// up as schema drift: a label or column change is invisible to a generated
// client, so it must not move SchemaHash.
func displayRich() *entity.DisplayConfig {
	return &entity.DisplayConfig{
		Singular:    "Invoice",
		Plural:      "Invoices",
		TitleFields: []string{"number"},
		Columns:     []string{"number", "amount", "status"},
		Nav:         &entity.EntityNav{Group: "billing", Icon: "receipt", Order: 1},
		Views:       []entity.ListView{{Key: "open", Where: `status = "open"`, Sort: "number ASC"}},
		Facets:      []string{"status"},
		Fields: map[string]entity.FieldDisplay{
			"number": {Label: "No", Placeholder: "INV-1"},
		},
		Form: &entity.EntityForm{
			Main: []entity.FormItem{{Row: []string{"number", "amount"}}},
			Side: []entity.FormItem{{Field: "status"}},
		},
		Card:      &entity.CardFields{Title: "number", Badge: "status"},
		PageSizes: []int{10, 25},
	}
}

func displayConfig(display *entity.DisplayConfig) entity.EntityConfig {
	return entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "number", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open"}, Default: "draft"},
		},
		Display: display,
	}
}

func TestDisplayDoesNotChangeSchemaHash(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "invoices", Config: displayConfig(nil)}})

	withDisplay := SchemaHash([]NamedConfig{{Name: "invoices", Config: displayConfig(displayRich())}})
	if withDisplay != base {
		t.Fatalf("adding Display changed the schema hash:\n%s\n%s", base, withDisplay)
	}

	// A DIFFERENT Display (other labels, columns, form) is the same
	// non-change.
	other := displayRich()
	other.Singular = "Bill"
	other.Columns = []string{"amount"}
	other.Fields["amount"] = entity.FieldDisplay{Help: "total"}
	other.Form.Side = nil
	other.PageSizes = []int{5}
	changed := SchemaHash([]NamedConfig{{Name: "invoices", Config: displayConfig(other)}})
	if changed != base {
		t.Fatalf("changing Display changed the schema hash:\n%s\n%s", base, changed)
	}

	// The test is not vacuous: a real schema change still moves the hash.
	cfg := displayConfig(nil)
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "memo", Type: schema.Text})
	if drifted := SchemaHash([]NamedConfig{{Name: "invoices", Config: cfg}}); drifted == base {
		t.Fatal("a schema change did not move the hash; the comparison above proves nothing")
	}
}
