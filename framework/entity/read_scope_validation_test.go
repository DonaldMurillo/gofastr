package entity

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ReadScope validation runs at Define, like the SearchFields and cursor
// checks: a typo must fail the app's start, never silently serve every row.
// TryEntity converts each panic into an error naming the entity and field.

func readScopeFields() []schema.Field {
	return []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
		{Name: "secret_code", Type: schema.String, Hidden: true},
	}
}

func TestReadScopeUnknownColumnPanics(t *testing.T) {
	expectDefinePanic(t, "not a declared field", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "stauts", Value: "published"}},
		}},
	})
}

func TestReadScopeHiddenColumnPanics(t *testing.T) {
	expectDefinePanic(t, "Hidden", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "secret_code", Value: "x"}},
		}},
	})
}

func TestReadScopeBadOpPanics(t *testing.T) {
	expectDefinePanic(t, "must be one of eq, neq, in, not_in", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "status", Op: "like", Value: "pub%"}},
		}},
	})
}

func TestReadScopeInWithoutValuesPanics(t *testing.T) {
	expectDefinePanic(t, "requires a non-empty Values", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "status", Op: "in"}},
		}},
	})
}

func TestReadScopeEqWithValuesPanics(t *testing.T) {
	expectDefinePanic(t, "must leave Values empty", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "status", Op: "eq", Value: "published", Values: []string{"x"}}},
		}},
	})
}

func TestReadScopeValidDeclarationOK(t *testing.T) {
	e := Define("posts", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{
				{Field: "status", Op: "in", Values: []string{"published", "archived"}},
				{Field: "title", Op: "neq", Value: "untitled"},
			},
		}},
	})
	if len(e.Config.Exposure.ReadScope.Filter) != 2 {
		t.Fatalf("ReadScope.Filter = %+v, want both predicates kept", e.Config.Exposure.ReadScope.Filter)
	}
}

func TestReadScopeEmptyOpMeansEq(t *testing.T) {
	// The empty op is the common declaration; anything but acceptance
	// would refuse the documented worked example.
	e := Define("posts", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "status", Value: "published"}},
		}},
	})
	if e.Config.Exposure.ReadScope.Filter[0].Field != "status" {
		t.Fatalf("ReadScope.Filter[0] = %+v", e.Config.Exposure.ReadScope.Filter[0])
	}
}

// The panic message must name the offending field so a failed start is
// actionable, the same contract the Default validation carries.
func TestReadScopePanicNamesField(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define did not panic")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, `"stauts"`) {
			t.Fatalf("panic = %v, want it to name the field", r)
		}
	}()
	Define("posts", EntityConfig{
		Fields: readScopeFields(),
		Exposure: &ExposureConfig{ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{{Field: "stauts", Value: "published"}},
		}},
	})
}

// Predicates AND, so two `in` predicates on one column are unsatisfiable and
// can only be a mistake. The renderer coalesces adjacent same-field IN runs
// into a single IN list, which turns that AND into an OR and serves EVERY row.
// A posture that silently WIDENS is the one shape registration must never
// accept, so it fails at definition.
func TestReadScopeRefusesTwoInPredicatesOnOneField(t *testing.T) {
	cfg := EntityConfig{
		Fields: []schema.Field{{Name: "status", Type: schema.String}},
		Exposure: &ExposureConfig{Public: true, ReadScope: &ReadScopeConfig{
			Filter: []RowPredicate{
				{Field: "status", Op: "in", Values: []string{"draft"}},
				{Field: "status", Op: "in", Values: []string{"published"}},
			},
		}},
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("two `in` predicates on one column were accepted; they render as a widened IN list that serves every row")
		}
		if !strings.Contains(fmt.Sprint(r), "status") {
			t.Errorf("the panic should name the field, got %v", r)
		}
	}()
	Define("notes", cfg.WithTimestamps(false))
}

// The combinations that remain legitimate must still register. `not_in` ANDs
// correctly across predicates (NOT IN (a) AND NOT IN (b) is NOT IN (a, b)), and
// a different operator on the same column is a real narrowing.
func TestReadScopeAllowsLegitimateSameFieldCombinations(t *testing.T) {
	cases := map[string][]RowPredicate{
		"two not_in on one field": {
			{Field: "status", Op: "not_in", Values: []string{"draft"}},
			{Field: "status", Op: "not_in", Values: []string{"archived"}},
		},
		"in plus neq on one field": {
			{Field: "status", Op: "in", Values: []string{"draft", "published"}},
			{Field: "status", Op: "neq", Value: "draft"},
		},
		"in on two different fields": {
			{Field: "status", Op: "in", Values: []string{"published"}},
			{Field: "kind", Op: "in", Values: []string{"post"}},
		},
	}
	for name, preds := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := EntityConfig{
				Fields: []schema.Field{
					{Name: "status", Type: schema.String},
					{Name: "kind", Type: schema.String},
				},
				Exposure: &ExposureConfig{Public: true, ReadScope: &ReadScopeConfig{Filter: preds}},
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("a legitimate declaration was refused: %v", r)
				}
			}()
			Define("notes_"+name, cfg.WithTimestamps(false))
		})
	}
}

// read_scope has a flat spelling at the entity root as well as a grouped one
// under `exposure:`, exactly like access. The guard that creates Exposure
// already named read_scope, but the merge loop beside it did not, so a flat
// declaration decoded to an Exposure with a nil ReadScope.
//
// The posture vanished silently, which is the worst way for this to fail: the
// blueprint says the rows are filtered, the app serves all of them, and
// nothing anywhere reports a problem.
func TestFlatReadScopeSurvivesDecoding(t *testing.T) {
	raw := []byte(`{
		"name": "posts",
		"crud": true,
		"read_scope": {
			"unrestricted": "content:review",
			"filter": [{"field": "status", "op": "eq", "value": "published"}]
		},
		"fields": [{"name": "status", "type": "string"}]
	}`)
	var d EntityDeclaration
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.Exposure == nil {
		t.Fatal("a flat read_scope did not create an Exposure")
	}
	if d.Exposure.ReadScope == nil {
		t.Fatal("a flat read_scope decoded to a nil ReadScope: the posture is silently dropped and every row is served")
	}
	if got := d.Exposure.ReadScope.Unrestricted; got != "content:review" {
		t.Errorf("Unrestricted = %q, want content:review", got)
	}
	if n := len(d.Exposure.ReadScope.Filter); n != 1 {
		t.Fatalf("Filter has %d predicates, want 1", n)
	}
	if f := d.Exposure.ReadScope.Filter[0]; f.Field != "status" || f.Value != "published" {
		t.Errorf("predicate = %+v, want status eq published", f)
	}
	// And it reaches the Config the framework actually enforces.
	cfg, err := d.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Exposure == nil || cfg.Exposure.ReadScope == nil || len(cfg.Exposure.ReadScope.Filter) != 1 {
		t.Errorf("Config lost the read scope: %+v", cfg.Exposure)
	}
}

// The grouped spelling keeps working, and declaring BOTH must be a conflict
// rather than a silent winner, which is what merging through the shared helper
// buys.
func TestGroupedReadScopeAndConflictDetection(t *testing.T) {
	grouped := []byte(`{
		"name": "posts",
		"exposure": {"crud": true, "read_scope": {"filter": [{"field": "status", "value": "published"}]}},
		"fields": [{"name": "status", "type": "string"}]
	}`)
	var g EntityDeclaration
	if err := json.Unmarshal(grouped, &g); err != nil {
		t.Fatal(err)
	}
	if g.Exposure == nil || g.Exposure.ReadScope == nil {
		t.Fatal("the grouped spelling lost its read scope")
	}

	both := []byte(`{
		"name": "posts",
		"read_scope": {"filter": [{"field": "status", "value": "published"}]},
		"exposure": {"read_scope": {"filter": [{"field": "status", "value": "draft"}]}},
		"fields": [{"name": "status", "type": "string"}]
	}`)
	var b EntityDeclaration
	err := json.Unmarshal(both, &b)
	if err == nil {
		t.Errorf("declaring read_scope flat AND grouped was accepted; one silently wins and the other is a lie: %+v", b.Exposure.ReadScope)
	}
}

// The mirror of the eq-with-Values check: a stray Value alongside Values on
// in/not_in is ignored by the builder, so the declaration and the enforced
// posture differ with nothing reporting it.
func TestReadScopeRefusesStrayValueOnIn(t *testing.T) {
	for _, op := range []string{"in", "not_in"} {
		t.Run(op, func(t *testing.T) {
			cfg := EntityConfig{
				Fields: []schema.Field{{Name: "status", Type: schema.String}},
				Exposure: &ExposureConfig{Public: true, ReadScope: &ReadScopeConfig{
					Filter: []RowPredicate{{Field: "status", Op: op, Value: "stray", Values: []string{"published"}}},
				}},
			}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("op %q accepted a stray Value that the builder ignores", op)
				}
				if !strings.Contains(fmt.Sprint(r), "stray") {
					t.Errorf("the panic should quote the ignored value, got %v", r)
				}
			}()
			Define("notes_stray_"+op, cfg.WithTimestamps(false))
		})
	}
}
