package entity

import (
	"errors"
	"slices"
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
	for _, id := range []string{"s1", "s1/c9/x", "s1/", "/c9", "s1/%zz"} {
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
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Define accepted PrimaryKey %q", pk)
				}
			}()
			Define("enrollments", EntityConfig{
				PrimaryKey: pk,
				Fields:     []schema.Field{{Name: "student_id", Type: schema.String}},
			})
		})
	}
}
