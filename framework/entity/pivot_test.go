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
			if tc.err {
				if err == nil || !strings.Contains(err.Error(), "2 versions") {
					t.Fatalf("PivotEntity = %v, %v; want an ambiguity error", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == "-" && got != nil:
				t.Fatalf("PivotEntity = %q, want nil", got.Version)
			case tc.want != "-" && (got == nil || got.Version != tc.want):
				t.Fatalf("PivotEntity = %v, want version %q", got, tc.want)
			}
		})
	}
}
