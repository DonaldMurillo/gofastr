package embedcheck

import (
	"slices"
	"testing"
)

func TestEachReachRouteFindsItsChild(t *testing.T) {
	findings, _ := loadAll(t, "reach")
	var got []string
	for _, f := range findings {
		got = append(got, f.Surface+"/"+f.Action)
	}
	slices.Sort(got)
	want := []string{"array/array", "body/body", "ctor/ctor", "helper/helper", "map/map", "nested/nested", "ptr/ptr", "slice/slice", "tree/tree"}
	if !slices.Equal(got, want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
}
