package entity

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// displayFields is the field set the display fixture below is written
// against: enough types to cover every guard (a relation, a decimal, an
// enum, dates, text, a bool and a Hidden column).
func displayFields() []schema.Field {
	return []schema.Field{
		{Name: "customer_id", Type: schema.Relation, To: "customers", Required: true},
		{Name: "number", Type: schema.String, Required: true},
		{Name: "amount", Type: schema.Decimal, Required: true},
		{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid"}},
		{Name: "recurring", Type: schema.Bool},
		{Name: "issued_on", Type: schema.Date},
		{Name: "due_on", Type: schema.Date},
		{Name: "paid_on", Type: schema.Date},
		{Name: "memo", Type: schema.Text},
		{Name: "secret", Type: schema.String, Hidden: true},
	}
}

// displayFixture is a DisplayConfig that exercises every setting and passes
// every guard. Tests copy it and break one thing.
func displayFixture() *DisplayConfig {
	return &DisplayConfig{
		Singular:    "Invoice",
		Plural:      "Invoices",
		TitleFields: []string{"number", "customer_id"},
		Description: "Money owed for work done",
		Columns:     []string{"number", "customer_id", "amount", "status", "due_on"},
		Nav:         &EntityNav{Group: "billing", Icon: "receipt", Order: 1},
		Facets:      []string{"status", "customer_id", "recurring"},
		Fields: map[string]FieldDisplay{
			"customer_id": {Label: "Customer"},
			"number":      {Placeholder: "INV-0001"},
			"paid_on":     {ShowWhen: `status = "paid"`},
		},
		Form: &EntityForm{
			Main: []FormItem{
				{Row: []string{"number", "amount"}},
				{Section: "dates", Items: []FormItem{
					{Row: []string{"issued_on", "due_on"}},
					{Field: "paid_on"},
				}},
				{Section: "notes", Collapsed: true, Help: "Internal notes", Items: []FormItem{{Field: "memo"}}},
			},
			Side: []FormItem{{Field: "status"}, {Field: "customer_id"}},
		},
		Card:      &CardFields{Title: "number", Subtitle: "customer_id", Badge: "status", Meta: []string{"amount", "due_on"}},
		Views:     []ListView{{Key: "open", Where: `status = "open"`, Sort: "due_on ASC"}, {Key: "overdue", Sort: "due_on ASC"}},
		PageSizes: []int{10, 25, 50},
	}
}

// displayEntityErr defines an entity carrying a mutated copy of the fixture
// and returns its Validate error.
func displayEntityErr(t *testing.T, mutate func(d *DisplayConfig)) error {
	t.Helper()
	d := displayFixture()
	if mutate != nil {
		mutate(d)
	}
	e := Define("invoices", EntityConfig{Fields: displayFields(), Display: d})
	return e.Validate()
}

func TestDisplayConfigPassesTheBootCheck(t *testing.T) {
	if err := displayEntityErr(t, nil); err != nil {
		t.Fatalf("fixture display rejected: %v", err)
	}
	// Nil display means every default and must stay valid (and stay nil
	// after Define, unlike the Scope group which Define populates).
	e := Define("invoices", EntityConfig{Fields: displayFields()})
	if e.Config.Display != nil {
		t.Fatalf("nil Display must stay nil after Define, got %+v", e.Config.Display)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("nil display rejected: %v", err)
	}
}

// Every name Display holds must be a declared, non-Hidden field. Unknown and
// Hidden are separate refusals, each naming the offender.
func TestDisplayNamesUnknownFieldRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(d *DisplayConfig)
		want   string
	}{
		{"title field", func(d *DisplayConfig) { d.TitleFields[1] = "custmer" }, `title_fields[1] names field "custmer", which the entity does not declare`},
		{"title field twice", func(d *DisplayConfig) { d.TitleFields[1] = "number" }, `title_fields list "number" more than once`},
		{"column", func(d *DisplayConfig) { d.Columns[2] = "amont" }, `columns[2] names field "amont", which the entity does not declare`},
		{"facet", func(d *DisplayConfig) { d.Facets[0] = "stats" }, `facets[0] names field "stats", which the entity does not declare`},
		{"field hint key", func(d *DisplayConfig) { d.Fields["nmber"] = FieldDisplay{Label: "No"} }, `fields[nmber] names field "nmber", which the entity does not declare`},
		{"card title", func(d *DisplayConfig) { d.Card.Title = "nbr" }, `card title names field "nbr", which the entity does not declare`},
		{"card meta", func(d *DisplayConfig) { d.Card.Meta[1] = "due" }, `card.meta[1] names field "due", which the entity does not declare`},
		{"form field", func(d *DisplayConfig) { d.Form.Side[0] = FormItem{Field: "stats"} }, `form side[0] names field "stats", which the entity does not declare`},
		{"form row member", func(d *DisplayConfig) { d.Form.Main[0].Row[1] = "amont" }, `form main[0] row names field "amont", which the entity does not declare`},
		// A view's Sort is no longer checked here: it is a DSL string,
		// parsed with dsl.ParseSort when the app registers the entity
		// (framework/display_check.go), the same grammar ?sort= parses.
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := displayEntityErr(t, tc.mutate)
			if err == nil {
				t.Fatalf("unknown field accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the offender: %q", err, tc.want)
			}
		})
	}
}

func TestDisplayNamesHiddenFieldRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(d *DisplayConfig)
	}{
		{"title field", func(d *DisplayConfig) { d.TitleFields[1] = "secret" }},
		{"column", func(d *DisplayConfig) { d.Columns[0] = "secret" }},
		{"facet", func(d *DisplayConfig) { d.Facets[0] = "secret" }},
		{"field hint key", func(d *DisplayConfig) { d.Fields["secret"] = FieldDisplay{Label: "No"} }},
		{"card badge", func(d *DisplayConfig) { d.Card.Badge = "secret" }},
		{"form side field", func(d *DisplayConfig) { d.Form.Side[0] = FormItem{Field: "secret"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := displayEntityErr(t, tc.mutate)
			if err == nil {
				t.Fatalf("Hidden field accepted")
			}
			if !strings.Contains(err.Error(), "which is Hidden") {
				t.Fatalf("error %q does not say the field is Hidden", err)
			}
		})
	}
}

// A facet is a one-click filter over a small value set, so only Enum, Bool
// and Relation fields may be one.
func TestDisplayFacetsMustBeEnumBoolOrRelation(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Facets = []string{"amount"} })
	if err == nil || !strings.Contains(err.Error(), `facets[0] "amount" must be an Enum, Bool or Relation field, not decimal`) {
		t.Fatalf("decimal facet refused wrongly: %v", err)
	}
	err = displayEntityErr(t, func(d *DisplayConfig) { d.Facets = []string{"memo"} })
	if err == nil || !strings.Contains(err.Error(), "must be an Enum, Bool or Relation") {
		t.Fatalf("text facet refused wrongly: %v", err)
	}
	// Enum, Bool and Relation all pass.
	if err := displayEntityErr(t, func(d *DisplayConfig) { d.Facets = []string{"recurring"} }); err != nil {
		t.Fatalf("bool facet refused: %v", err)
	}
}

func TestDisplayKeysMustBeLowercaseSlugs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(d *DisplayConfig)
	}{
		{"uppercase view key", func(d *DisplayConfig) { d.Views[0].Key = "Open" }},
		{"dotted view key", func(d *DisplayConfig) { d.Views[0].Key = "over.due" }},
		{"digit-leading view key", func(d *DisplayConfig) { d.Views[0].Key = "9open" }},
		{"empty view key", func(d *DisplayConfig) { d.Views[0].Key = "" }},
		{"uppercase section key", func(d *DisplayConfig) { d.Form.Main[1].Section = "Dates" }},
		{"spaced nav group", func(d *DisplayConfig) { d.Nav.Group = "billing ops" }},
		{"dotted nav group", func(d *DisplayConfig) { d.Nav.Group = "billing.ops" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := displayEntityErr(t, tc.mutate)
			if err == nil || !strings.Contains(err.Error(), "must be a lowercase slug") {
				t.Fatalf("bad key accepted: %v", err)
			}
		})
	}
}

// all and deleted belong to the screens; no key may take them.
func TestDisplayReservedKeysRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(d *DisplayConfig)
	}{
		{"view all", func(d *DisplayConfig) { d.Views[0].Key = "all" }},
		{"view deleted", func(d *DisplayConfig) { d.Views[1].Key = "deleted" }},
		{"section all", func(d *DisplayConfig) { d.Form.Main[1].Section = "all" }},
		{"nav group deleted", func(d *DisplayConfig) { d.Nav.Group = "deleted" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := displayEntityErr(t, tc.mutate)
			if err == nil || !strings.Contains(err.Error(), "is reserved") {
				t.Fatalf("reserved key accepted: %v", err)
			}
		})
	}
}

func TestDisplayViewKeysUnique(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Views[1].Key = "open" })
	if err == nil || !strings.Contains(err.Error(), `declares view "open" more than once`) {
		t.Fatalf("duplicate view key accepted: %v", err)
	}
}

func TestDisplayAtMostOneDefaultView(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Views[0].Default = true
		d.Views[1].Default = true
	})
	if err == nil || !strings.Contains(err.Error(), "more than one default view") {
		t.Fatalf("two default views accepted: %v", err)
	}
	if err := displayEntityErr(t, func(d *DisplayConfig) { d.Views[0].Default = true }); err != nil {
		t.Fatalf("one default view refused: %v", err)
	}
}

// A facet filters on the field, so it may not name a NoQuery field. (A
// view's Sort is a DSL string: its NoQuery refusal moved to
// dsl.ParseSort at registration, framework/display_check.go.)
func TestDisplayRefusesNoQueryFacet(t *testing.T) {
	fields := displayFields()
	for i := range fields {
		if fields[i].Name == "recurring" {
			fields[i].NoQuery = true
		}
	}
	d := displayFixture()
	d.Facets = append(d.Facets, "recurring")
	err := Define("invoices", EntityConfig{Fields: fields, Display: d}).Validate()
	if err == nil || !strings.Contains(err.Error(), "which is NoQuery") {
		t.Fatalf("NoQuery facet accepted: %v", err)
	}
}

// Columns, Facets and PageSizes are menus; a repeated entry is a typo
// that would render a duplicated column, chip or size, and the refusal
// names the duplicate.
func TestDisplayRefusesDuplicateColumns(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Columns = append(d.Columns, d.Columns[0]) })
	if err == nil || !strings.Contains(err.Error(), `columns list "number" more than once`) {
		t.Fatalf("duplicate column accepted: %v", err)
	}
}

func TestDisplayRefusesDuplicateFacets(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Facets = append(d.Facets, d.Facets[0]) })
	if err == nil || !strings.Contains(err.Error(), `facets list "status" more than once`) {
		t.Fatalf("duplicate facet accepted: %v", err)
	}
}

func TestDisplayRefusesDuplicatePageSizes(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.PageSizes = append(d.PageSizes, d.PageSizes[0]) })
	if err == nil || !strings.Contains(err.Error(), `page_sizes list 10 more than once`) {
		t.Fatalf("duplicate page size accepted: %v", err)
	}
}

// As documents how a view's rows are drawn: "table" (the default when
// empty) or "cards". Anything else is a typo no screen would match.
func TestDisplayViewAsMustBeTableOrCards(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Views[0].As = "gallery" })
	if err == nil || !strings.Contains(err.Error(), `view "open" as "gallery" must be "table" or "cards"`) {
		t.Fatalf("unknown As accepted: %v", err)
	}
	for _, as := range []string{"", "table", "cards"} {
		if err := displayEntityErr(t, func(d *DisplayConfig) { d.Views[0].As = as }); err != nil {
			t.Fatalf("As %q refused: %v", as, err)
		}
	}
}

func TestDisplayRefusesEmptySection(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Main[2].Items = nil })
	if err == nil || !strings.Contains(err.Error(), `section "notes" holds no items`) {
		t.Fatalf("empty section accepted: %v", err)
	}
}

func TestFormItemsSetExactlyOneShape(t *testing.T) {
	empty := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Side[0] = FormItem{} })
	if empty == nil || !strings.Contains(empty.Error(), "sets 0 of Field, Row and Section") {
		t.Fatalf("empty form item accepted: %v", empty)
	}
	both := displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Side[0] = FormItem{Field: "memo", Row: []string{"memo"}}
	})
	if both == nil || !strings.Contains(both.Error(), "sets 2 of Field, Row and Section") {
		t.Fatalf("field+row item accepted: %v", both)
	}
}

func TestFormRowsHoldOneToThreeDistinctFields(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Main[0] = FormItem{Row: []string{"number", "amount", "status", "memo"}}
	})
	if err == nil || !strings.Contains(err.Error(), "a row holds one to three") {
		t.Fatalf("four-field row accepted: %v", err)
	}
	err = displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Main[0] = FormItem{Row: []string{"number", "number"}}
	})
	if err == nil || !strings.Contains(err.Error(), `lists field "number" twice`) {
		t.Fatalf("row listing one field twice accepted: %v", err)
	}
	// A single-field row is fine.
	if err := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Main[0] = FormItem{Row: []string{"number"}} }); err != nil {
		t.Fatalf("one-field row refused: %v", err)
	}
}

func TestFormOnlySectionsCarryItemsHelpCollapsed(t *testing.T) {
	for _, tc := range []struct {
		name string
		item FormItem
	}{
		{"field with help", FormItem{Field: "memo", Help: "note"}},
		{"field with items", FormItem{Field: "memo", Items: []FormItem{{Field: "status"}}}},
		{"row collapsed", FormItem{Row: []string{"number"}, Collapsed: true}},
		{"row with help", FormItem{Row: []string{"number"}, Help: "note"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Side[0] = tc.item })
			if err == nil || !strings.Contains(err.Error(), "only a section carries Items, Help or Collapsed") {
				t.Fatalf("non-section extras accepted: %v", err)
			}
		})
	}
}

func TestFormSectionsNestAtMostTwoDeep(t *testing.T) {
	// A section inside a section is fine.
	if err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Main[1].Items = append(d.Form.Main[1].Items, FormItem{
			Section: "inner", Items: []FormItem{{Field: "recurring"}},
		})
	}); err != nil {
		t.Fatalf("two-deep section refused: %v", err)
	}
	// Three deep is not.
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Main[1].Items = append(d.Form.Main[1].Items, FormItem{
			Section: "inner",
			Items: []FormItem{{
				Section: "innermost", Items: []FormItem{{Field: "recurring"}},
			}},
		})
	})
	if err == nil || !strings.Contains(err.Error(), `section "innermost" nests 3 deep`) {
		t.Fatalf("three-deep section accepted: %v", err)
	}
}

func TestFormFieldPlacedOnceAcrossMainAndSide(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Side[1] = FormItem{Field: "number"} })
	if err == nil || !strings.Contains(err.Error(), `places field "number" more than once`) {
		t.Fatalf("field placed twice accepted: %v", err)
	}
	// Twice inside Main is the same refusal.
	err = displayEntityErr(t, func(d *DisplayConfig) {
		d.Form.Main[1].Items = append(d.Form.Main[1].Items, FormItem{Field: "number"})
	})
	if err == nil || !strings.Contains(err.Error(), `places field "number" more than once`) {
		t.Fatalf("field placed twice in Main accepted: %v", err)
	}
}

func TestFormSectionKeysUnique(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) { d.Form.Main[2].Section = "dates" })
	if err == nil || !strings.Contains(err.Error(), `declares section "dates" more than once`) {
		t.Fatalf("duplicate section key accepted: %v", err)
	}
}

// Omit on a Required field with no supplied value leaves no way to create
// the record from a screen.
func TestDisplayOmitRefusedOnRequiredNoDefault(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["number"] = FieldDisplay{Omit: true}
	})
	if err == nil || !strings.Contains(err.Error(), `fields[number] omits a Required field with no Default`) {
		t.Fatalf("omit on Required accepted: %v", err)
	}
	// A Default (status) or a plain optional field (memo) may be omitted.
	if err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["status"] = FieldDisplay{Omit: true}
		d.Fields["memo"] = FieldDisplay{Omit: true}
	}); err != nil {
		t.Fatalf("omit on defaulted or optional field refused: %v", err)
	}
}

// Locked draws a field read-only and the screens' save path drops a
// Locked key before the write, so on create the value never submits —
// the same dead end as Omit for a Required field with no supplied
// value.
func TestDisplayLockedRefusedOnRequiredNoDefault(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["number"] = FieldDisplay{Locked: true}
	})
	if err == nil || !strings.Contains(err.Error(), `fields[number] locks a Required field with no Default`) {
		t.Fatalf("locked on Required accepted: %v", err)
	}
	if err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["memo"] = FieldDisplay{Locked: true}
	}); err != nil {
		t.Fatalf("locked on optional field refused: %v", err)
	}
}

// ShowWhen hides the field's region while its condition does not hold,
// and when.js disables a hidden region's controls, so they never
// submit: a Required field with no supplied value behind a ShowWhen
// cannot be created from a screen whenever the condition starts false.
func TestDisplayShowWhenRefusedOnRequiredNoDefault(t *testing.T) {
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["amount"] = FieldDisplay{ShowWhen: `status = "paid"`}
	})
	if err == nil || !strings.Contains(err.Error(), `fields[amount] hides behind show_when on a Required field with no Default`) {
		t.Fatalf("show_when on Required accepted: %v", err)
	}
	// paid_on is optional in the fixture and already carries a
	// ShowWhen: the fixture passing TestDisplayConfigPassesTheBootCheck
	// is the positive case.
}

func TestDisplayInputMustBeAKey(t *testing.T) {
	for _, kind := range []string{"Money", "a b", "../x"} {
		err := displayEntityErr(t, func(d *DisplayConfig) {
			d.Fields["memo"] = FieldDisplay{Input: kind}
		})
		if err == nil || !strings.Contains(err.Error(), "fields[memo].input") {
			t.Fatalf("input %q accepted: %v", kind, err)
		}
	}
	err := displayEntityErr(t, func(d *DisplayConfig) {
		d.Fields["memo"] = FieldDisplay{Input: "markdown"}
	})
	if err != nil {
		t.Fatalf("input markdown refused: %v", err)
	}
}

func TestDisplayPageSizesPositiveAndCapped(t *testing.T) {
	for _, size := range []int{0, -5} {
		err := displayEntityErr(t, func(d *DisplayConfig) { d.PageSizes = []int{10, size} })
		if err == nil || !strings.Contains(err.Error(), "every entry must be positive") {
			t.Fatalf("page size %d accepted: %v", size, err)
		}
	}
	// Above Pagination.MaxListLimit is refused when the entity sets one.
	capped := displayFixture()
	capped.PageSizes = []int{10, 500}
	e := Define("invoices", EntityConfig{
		Fields:     displayFields(),
		Pagination: &PaginationConfig{MaxListLimit: 100},
		Display:    capped,
	})
	err := e.Validate()
	if err == nil || !strings.Contains(err.Error(), "above Pagination.MaxListLimit 100") {
		t.Fatalf("page size above the cap accepted: %v", err)
	}
	// With no cap set (MaxListLimit 0), the same sizes pass.
	uncapped := Define("invoices", EntityConfig{
		Fields:  displayFields(),
		Display: displayFixture(),
	})
	if err := uncapped.Validate(); err != nil {
		t.Fatalf("page size without a cap refused: %v", err)
	}
}

// Define deep-copies Display: a caller that edits its value afterwards —
// including every nested slice, map and pointer — changes nothing the app
// checked or serves.
func TestDisplayDeepCopiedAtDefine(t *testing.T) {
	d := displayFixture()
	e := Define("invoices", EntityConfig{Fields: displayFields(), Display: d})
	if err := e.Validate(); err != nil {
		t.Fatalf("fixture rejected: %v", err)
	}

	// Mutate every level of the caller's value.
	d.Singular = "Changed"
	d.Columns[0] = "changed"
	d.Facets[0] = "changed"
	d.PageSizes[0] = 999
	d.NoBulk = true
	d.Nav.Group = "changed"
	d.Nav.Order = 99
	d.Views[0].Key = "changed"
	d.Views[0].Sort = "changed ASC"
	d.Card.Meta[0] = "changed"
	d.Card.Badge = "changed"
	d.Form.Main[0].Row[0] = "changed"
	d.Form.Main[1].Section = "changed"
	d.Form.Main[2].Collapsed = false
	d.Form.Main[2].Help = "changed"
	d.Form.Main[1].Items[0].Row[0] = "changed"
	d.Form.Side[0].Field = "changed"
	d.Fields["number"] = FieldDisplay{Label: "changed"}
	d.Fields["extra"] = FieldDisplay{Omit: true}
	delete(d.Fields, "customer_id")

	got := e.Config.Display
	if got.Singular != "Invoice" || got.NoBulk {
		t.Fatalf("scalar not copied: %+v", got)
	}
	if got.Columns[0] != "number" || got.Facets[0] != "status" || got.PageSizes[0] != 10 {
		t.Fatalf("slice not copied: %+v", got)
	}
	if got.Nav == nil || got.Nav.Group != "billing" || got.Nav.Order != 1 {
		t.Fatalf("nav pointer not copied: %+v", got.Nav)
	}
	if got.Views[0].Key != "open" || got.Views[0].Sort != "due_on ASC" {
		t.Fatalf("view slice not copied: %+v", got.Views)
	}
	if got.Card.Meta[0] != "amount" || got.Card.Badge != "status" {
		t.Fatalf("card not copied: %+v", got.Card)
	}
	if got.Form.Main[0].Row[0] != "number" ||
		got.Form.Main[1].Section != "dates" || got.Form.Main[1].Items[0].Row[0] != "issued_on" ||
		got.Form.Main[2].Section != "notes" || !got.Form.Main[2].Collapsed || got.Form.Main[2].Help != "Internal notes" ||
		got.Form.Side[0].Field != "status" {
		t.Fatalf("form not copied deeply: %+v", got.Form)
	}
	if got.Fields["number"].Label != "" || got.Fields["customer_id"].Label != "Customer" || len(got.Fields) != 3 {
		t.Fatalf("field hints map not copied: %+v", got.Fields)
	}
	// The checked copy still validates after the caller's edits.
	if err := e.Validate(); err != nil {
		t.Fatalf("registered copy invalidated by caller edits: %v", err)
	}
}

// The declaration reads a display key with the snake_case spelling, and the
// form's three item shapes decode (a bare string, a row, a section with
// nested items of all three shapes again).
func TestDisplayDeclarationDecodes(t *testing.T) {
	raw := `{
	  "name": "invoices",
	  "fields": [
	    {"name": "number", "type": "string", "required": true},
	    {"name": "amount", "type": "decimal", "required": true},
	    {"name": "status", "type": "enum", "default": "draft", "values": ["draft", "open", "paid"]},
	    {"name": "customer_id", "type": "relation", "to": "customers", "required": true},
	    {"name": "issued_on", "type": "date"},
	    {"name": "due_on", "type": "date"},
	    {"name": "paid_on", "type": "date"},
	    {"name": "memo", "type": "text"}
	  ],
	  "display": {
	    "singular": "Invoice",
	    "plural": "Invoices",
	    "title_fields": ["number", "customer_id"],
	    "description": "Money owed",
	    "columns": ["number", "amount", "status"],
	    "nav": {"group": "billing", "icon": "receipt", "order": 1, "hide": false},
	    "views": [
	      {"key": "open", "label": "Open", "where": "status = \"open\"", "sort": "due_on ASC", "as": "cards", "default": true}
	    ],
	    "facets": ["status", "customer_id"],
	    "form": {
	      "main": [
	        "memo",
	        {"row": ["issued_on", "due_on"]},
	        {"section": "dates", "help": "When", "collapsed": true, "items": ["paid_on", {"row": ["number", "amount"]}]}
	      ],
	      "side": ["status"]
	    },
	    "card": {"title": "number", "subtitle": "customer_id", "badge": "status", "meta": ["amount"]},
	    "fields": {"number": {"label": "No", "help": "the number", "placeholder": "INV-1"}, "memo": {"locked": true, "show_when": "status = \"paid\""}},
	    "page_sizes": [10, 25],
	    "no_duplicate": true,
	    "no_bulk": false
	  }
	}`
	var decl EntityDeclaration
	if err := json.Unmarshal([]byte(raw), &decl); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := decl.Display
	if d == nil {
		t.Fatal("display not decoded")
	}
	if d.Singular != "Invoice" || d.Plural != "Invoices" || !slices.Equal(d.TitleFields, []string{"number", "customer_id"}) || d.Description != "Money owed" {
		t.Fatalf("names not decoded: %+v", d)
	}
	if len(d.Columns) != 3 || d.Nav == nil || d.Nav.Group != "billing" || d.Nav.Icon != "receipt" || d.Nav.Order != 1 || d.Nav.Hide {
		t.Fatalf("columns/nav not decoded: %+v", d.Nav)
	}
	v := d.Views[0]
	if v.Key != "open" || v.Label != "Open" || v.Where != `status = "open"` || v.Sort != "due_on ASC" || v.As != "cards" || !v.Default {
		t.Fatalf("view not decoded: %+v", v)
	}
	if len(d.Facets) != 2 || !d.NoDuplicate || d.NoBulk || len(d.PageSizes) != 2 {
		t.Fatalf("facets/flags/page_sizes not decoded: %+v", d)
	}
	if len(d.Form.Main) != 3 {
		t.Fatalf("form main not decoded: %+v", d.Form)
	}
	if d.Form.Main[0].Field != "memo" {
		t.Fatalf("bare-string form item not decoded as a field: %+v", d.Form.Main[0])
	}
	if len(d.Form.Main[1].Row) != 2 {
		t.Fatalf("row form item not decoded: %+v", d.Form.Main[1])
	}
	sec := d.Form.Main[2]
	if sec.Section != "dates" || sec.Help != "When" || !sec.Collapsed || len(sec.Items) != 2 ||
		sec.Items[0].Field != "paid_on" || len(sec.Items[1].Row) != 2 {
		t.Fatalf("section form item not decoded: %+v", sec)
	}
	if d.Form.Side[0].Field != "status" {
		t.Fatalf("side not decoded: %+v", d.Form.Side)
	}
	fd := d.Fields["number"]
	if fd.Label != "No" || fd.Help != "the number" || fd.Placeholder != "INV-1" || fd.Locked || fd.Omit ||
		fd.ShowWhen != "" {
		t.Fatalf("field hints not decoded: %+v", fd)
	}
	// Locked and ShowWhen decode too — on the optional field, where the
	// boot check takes them (a Required field with no Default cannot be
	// locked or conditionally hidden; see the refusal tests).
	hints := d.Fields["memo"]
	if !hints.Locked || hints.Omit || hints.ShowWhen != `status = "paid"` {
		t.Fatalf("locked/show_when hints not decoded: %+v", hints)
	}
	if d.Card == nil || d.Card.Title != "number" || d.Card.Subtitle != "customer_id" || d.Card.Badge != "status" || len(d.Card.Meta) != 1 {
		t.Fatalf("card not decoded: %+v", d.Card)
	}

	// Config() carries Display through, and the result passes the boot
	// check once defined.
	cfg, err := decl.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Display == nil || cfg.Display.Singular != "Invoice" {
		t.Fatalf("Config did not carry Display through: %+v", cfg.Display)
	}
	if err := Define("invoices", cfg).Validate(); err != nil {
		t.Fatalf("declared display rejected: %v", err)
	}
}

// A decoded form round-trips: a field item marshals back to a bare string.
func TestFormItemRoundTrips(t *testing.T) {
	items := []FormItem{
		{Field: "memo"},
		{Row: []string{"a", "b"}},
		{Section: "dates", Help: "h", Collapsed: true, Items: []FormItem{{Field: "paid_on"}}},
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back []FormItem
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back[0].Field != "memo" || len(back[1].Row) != 2 || back[2].Section != "dates" || !back[2].Collapsed || back[2].Items[0].Field != "paid_on" {
		t.Fatalf("round trip lost shape: %+v", back)
	}
}

// Unknown keys inside display are refused at every level, and a form item
// object takes only row, section, help, collapsed and items.
func TestDisplayDeclarationRefusesUnknownKeys(t *testing.T) {
	entity := func(display string) string {
		return `{"name": "invoices", "fields": [{"name": "number", "type": "string"}], "display": ` + display + `}`
	}
	for _, tc := range []struct {
		name    string
		display string
		want    string
	}{
		{"top level", `{"singular": "Invoice", "colums": []}`, "colums"},
		{"nav", `{"nav": {"group": "b", "grp": "x"}}`, "grp"},
		{"view", `{"views": [{"key": "open", "wher": "x"}]}`, "wher"},
		{"card", `{"card": {"titl": "number"}}`, "titl"},
		{"field hint", `{"fields": {"number": {"labl": "No"}}}`, "labl"},
		{"form item", `{"form": {"main": [{"row": ["number"], "widgh": 3}]}}`, "widgh"},
		{"form item field key", `{"form": {"main": [{"field": "number"}]}}`, "field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var decl EntityDeclaration
			err := json.Unmarshal([]byte(entity(tc.display)), &decl)
			if err == nil {
				t.Fatalf("unknown key accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the unknown key %q", err, tc.want)
			}
		})
	}
}
