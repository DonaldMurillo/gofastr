package entity

import (
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
