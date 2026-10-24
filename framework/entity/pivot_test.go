package entity

import (
	"fmt"
	"strings"
	"testing"
)

type pivotReg []*Entity

func (r pivotReg) All() map[string]*Entity {
	out := map[string]*Entity{}
	for _, e := range r {
		out[e.GetName()+"@"+e.Version] = e
	}
	return out
}

func (r pivotReg) AllSorted() []*Entity { return r }

func (r pivotReg) Get(name string) (*Entity, error) {
	return nil, fmt.Errorf("not found: %s", name)
}

func pivotAt(version string) *Entity {
	return &Entity{Config: EntityConfig{Name: "enrollments", Table: "enrollments"}, Version: version}
}

func TestPivotEntityPicksVersion(t *testing.T) {
	v1 := &Entity{Config: EntityConfig{Name: "students"}, Version: "/v1"}
	cases := []struct {
		name string
		reg  pivotReg
		want string // version of the pivot picked; "-" for none
		err  bool
	}{
		{"source version", pivotReg{pivotAt(""), pivotAt("/v1"), pivotAt("/v2")}, "/v1", false},
		{"unversioned", pivotReg{pivotAt("/v2"), pivotAt(""), pivotAt("/v3")}, "", false},
		{"only one", pivotReg{pivotAt("/v2")}, "/v2", false},
		{"several others", pivotReg{pivotAt("/v2"), pivotAt("/v3")}, "", true},
		{"none", pivotReg{}, "-", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PivotEntity(tc.reg, v1, "Enrollments")
			checkPivot(t, got, err, tc.want, tc.err)
		})
	}
	// The guards: no registry or no table names no pivot, and a source
	// with no version takes the unversioned one.
	two := pivotReg{pivotAt("/v1"), pivotAt("")}
	for name, tc := range map[string]struct {
		reg   Registry
		src   *Entity
		table string
		want  string
	}{
		"nil registry": {nil, v1, "Enrollments", "-"},
		"empty table":  {two, v1, "", "-"},
		"nil source":   {two, nil, "Enrollments", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := PivotEntity(tc.reg, tc.src, tc.table)
			checkPivot(t, got, err, tc.want, false)
		})
	}
}

// checkPivot fails unless PivotEntity picked the pivot at version want
// ("-" for none), or failed on an ambiguity when wantErr.
func checkPivot(t *testing.T, got *Entity, err error, want string, wantErr bool) {
	t.Helper()
	if wantErr {
		if err == nil || !strings.Contains(err.Error(), "2 versions") {
			t.Fatalf("PivotEntity = %v, %v; want an ambiguity error", got, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case want == "-" && got != nil:
		t.Fatalf("PivotEntity = %q, want nil", got.Version)
	case want != "-" && (got == nil || got.Version != want):
		t.Fatalf("PivotEntity = %v, want version %q", got, want)
	}
}
