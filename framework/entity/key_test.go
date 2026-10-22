package entity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestJoinKeySingleIsRaw(t *testing.T) {
	for _, v := range []string{"7", "a/b", "50%", "x y"} {
		if got := JoinKey([]string{v}); got != v {
			t.Errorf("JoinKey(%q) = %q, want it unchanged", v, got)
		}
		got, err := SplitKey(v, 1)
		if err != nil || !slices.Equal(got, []string{v}) {
			t.Errorf("SplitKey(%q, 1) = %q, %v", v, got, err)
		}
	}
}

func TestJoinKeyRoundTrip(t *testing.T) {
	for _, parts := range [][]string{
		{"s1", "c9"},
		{"a/b", "c"},
		{"50%", "x y", "é"},
	} {
		id := JoinKey(parts)
		got, err := SplitKey(id, len(parts))
		if err != nil || !slices.Equal(got, parts) {
			t.Errorf("SplitKey(JoinKey(%q)) = %q, %v (id %q)", parts, got, err, id)
		}
	}
	if got := JoinKey([]string{"a/b", "c"}); got != "a%2Fb/c" {
		t.Errorf("a slash inside a value must be escaped: %q", got)
	}
}

func TestSplitKeyRefusesBadIDs(t *testing.T) {
	// "%61" and "a%40b" spell values JoinKey writes as "a" and "a@b": a
	// second spelling of one record is refused, never decoded.
	for _, id := range []string{"s1", "s1/c9/x", "s1/", "/c9", "s1/%zz", "%61/b", "a%40b/c"} {
		if _, err := SplitKey(id, 2); !errors.Is(err, ErrBadKey) {
			t.Errorf("SplitKey(%q, 2) err = %v, want ErrBadKey", id, err)
		}
	}
}

func TestRowKey(t *testing.T) {
	camel := func(c string) string {
		if c == "course_id" {
			return "courseId"
		}
		return c
	}
	row := map[string]any{"student": "s1", "courseId": 9}
	if got, ok := RowKey(row, []string{"student", "course_id"}, camel); !ok || got != "s1/9" {
		t.Errorf("RowKey = %q, %v", got, ok)
	}
	if _, ok := RowKey(row, []string{"student", "term"}, camel); ok {
		t.Error("a missing key column must not name a record")
	}
	if _, ok := RowKey(map[string]any{"id": nil}, []string{"id"}, nil); ok {
		t.Error("a nil key value must not name a record")
	}
}

func TestKeyColumnsDefault(t *testing.T) {
	if got := (&Entity{}).KeyColumns(); !slices.Equal(got, []string{"id"}) {
		t.Errorf("KeyColumns() = %q", got)
	}
	if got := (&Entity{Config: EntityConfig{PrimaryKey: []string{"code"}}}).KeyColumns(); !slices.Equal(got, []string{"code"}) {
		t.Errorf("KeyColumns() = %q", got)
	}
}

func TestCompositeKeyNoID(t *testing.T) {
	e := Define("enrollments", EntityConfig{
		PrimaryKey: []string{"student_id", "course_id"},
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String},
			{Name: "course_id", Type: schema.String},
		},
	})
	if got := e.KeyColumns(); !slices.Equal(got, []string{"student_id", "course_id"}) {
		t.Fatalf("KeyColumns() = %q", got)
	}
	for _, f := range e.GetFields() {
		if f.Name == "id" {
			t.Fatal("a composite key without id still got an id column")
		}
	}
}

func TestPrimaryKeyRefusals(t *testing.T) {
	for name, pk := range map[string][]string{
		"undeclared": {"student_id", "nope"},
		"repeated":   {"student_id", "student_id"},
		"hidden":     {"student_id", "secret"},
		"noquery":    {"student_id", "opaque"},
		"tenant":     {"tenant_id", "student_id"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Define accepted PrimaryKey %q", pk)
				}
			}()
			Define("enrollments", EntityConfig{
				PrimaryKey: pk,
				Fields: []schema.Field{
					{Name: "student_id", Type: schema.String},
					{Name: "secret", Type: schema.String, Hidden: true},
					{Name: "opaque", Type: schema.String, NoQuery: true},
				},
				Scope: &ScopeConfig{MultiTenant: true},
				// A CursorField keeps the key out of the cursor check, so
				// only the key's own check refuses it.
				Pagination: &PaginationConfig{CursorField: "student_id"},
			})
		})
	}
}

func TestKeyColumnTypes(t *testing.T) {
	define := func(typ schema.FieldType) (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		Define("readings", EntityConfig{
			PrimaryKey: []string{"sensor", "at"},
			Fields: []schema.Field{
				{Name: "sensor", Type: schema.String},
				{Name: "at", Type: typ, Values: []string{"a"}},
			},
		})
		return ""
	}
	for _, typ := range []schema.FieldType{schema.Float, schema.Decimal, schema.Bool, schema.Date, schema.Timestamp, schema.JSON} {
		if msg := define(typ); !strings.Contains(msg, "more than one spelling") {
			t.Errorf("type %v: Define = %q, want the spelling refusal", typ, msg)
		}
	}
	for _, typ := range []schema.FieldType{schema.String, schema.Text, schema.Int, schema.Enum, schema.UUID} {
		if msg := define(typ); msg != "" {
			t.Errorf("type %v: Define refused a keyable column: %s", typ, msg)
		}
	}
}

func TestKeyColumnRawType(t *testing.T) {
	define := func(f schema.Field) (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		f.Name = "at"
		Define("readings", EntityConfig{
			PrimaryKey: []string{"sensor", "at"},
			Fields:     []schema.Field{{Name: "sensor", Type: schema.String}, f},
		})
		return ""
	}
	for _, f := range []schema.Field{
		{Type: schema.UUID, RawType: "UUID"},
		{Type: schema.String, RawType: "CITEXT"},
		{Type: schema.Int, RawType: "NUMERIC(10,0)"},
		{Type: schema.Int, RawType: "ınt"},
	} {
		if msg := define(f); !strings.Contains(msg, "more than one spelling") {
			t.Errorf("%v %q: Define = %q, want the spelling refusal", f.Type, f.RawType, msg)
		}
	}
	for _, raw := range []string{"BIGINT", "integer", " bigserial "} {
		if msg := define(schema.Field{Type: schema.Int, RawType: raw}); msg != "" {
			t.Errorf("Int %q: Define refused an integer key: %s", raw, msg)
		}
	}
}

func TestKeyValues(t *testing.T) {
	fields := []schema.Field{{Name: "room", Type: schema.Int}, {Name: "code", Type: schema.String}}
	cols := []string{"room", "code"}
	got, err := KeyValues("12/01", cols, fields)
	if err != nil || !slices.Equal(got, []any{int64(12), "01"}) {
		t.Fatalf("KeyValues(12/01) = %v, %v; want [12 01]", got, err)
	}
	for _, id := range []string{"012/a", "+12/a", "12.0/a", "1e2/a", "12"} {
		if _, err := KeyValues(id, cols, fields); !errors.Is(err, ErrBadKey) {
			t.Errorf("KeyValues(%q) = %v, want ErrBadKey", id, err)
		}
	}
	if _, err := KeyValues("x", nil, fields); !errors.Is(err, ErrBadKey) {
		t.Errorf("KeyValues on a keyless table = %v, want ErrBadKey", err)
	}
}
