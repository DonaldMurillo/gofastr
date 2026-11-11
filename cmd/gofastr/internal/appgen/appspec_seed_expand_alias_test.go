package appgen

import (
	"maps"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

//gofastr:test-reads /cmd/gofastr/internal/appgen/testdata/union/gofastr.yml

// Rendering expands a seed recipe (count + weights) into rows. It did so
// through the caller's Seed slice, so the spec a caller held came back
// expanded, and rendering it again expanded it again: stubs.go then
// carried Authored 8 over 14 rows for a recipe of 6 over 2, and pack
// read the duplicates back as authored data.
func TestRenderLeavesSeedRecipeAlone(t *testing.T) {
	spec, err := appspec.LoadAppSpec(filepath.Join("testdata", "union", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := 0
	for _, s := range spec.Seed {
		if s.Count > 0 {
			recipes++
		}
	}
	if recipes == 0 {
		t.Fatal("the union spec seeds no recipe; the test reads nothing")
	}
	before := deepCopySeed(spec.Seed)
	stubs := func() string {
		t.Helper()
		files, err := RenderAppSpecCode(spec)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Name == "stubs.go" {
				return f.Content
			}
		}
		t.Fatal("no stubs.go rendered")
		return ""
	}
	first := stubs()
	if !reflect.DeepEqual(spec.Seed, before) {
		t.Errorf("render changed the caller's seed:\n got  %#v\n want %#v", spec.Seed, before)
	}
	if second := stubs(); second != first {
		t.Error("a second render of the same spec wrote a different stubs.go")
	}
}

// deepCopySeed copies every level a render could write through: the
// rows slice, each row map and its nested values, and the weights.
func deepCopySeed(in []appspec.AppSpecSeedEntity) []appspec.AppSpecSeedEntity {
	out := make([]appspec.AppSpecSeedEntity, len(in))
	for i, s := range in {
		out[i] = s
		if s.Rows != nil {
			out[i].Rows = make([]map[string]any, len(s.Rows))
			for j, row := range s.Rows {
				out[i].Rows[j], _ = deepCopyValue(row).(map[string]any)
			}
		}
		if s.Weights != nil {
			out[i].Weights = make(map[string]map[string]int, len(s.Weights))
			for col, w := range s.Weights {
				out[i].Weights[col] = maps.Clone(w)
			}
		}
	}
	return out
}

func deepCopyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if x == nil {
			return x
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepCopyValue(e)
		}
		return out
	case []any:
		if x == nil {
			return x
		}
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyValue(e)
		}
		return out
	}
	return v
}
