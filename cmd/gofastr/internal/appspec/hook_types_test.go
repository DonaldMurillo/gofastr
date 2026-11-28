package appspec

import (
	"maps"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestHookTypesMatchKiln(t *testing.T) {
	got := slices.Sorted(maps.Keys(AppSpecHookTypes))
	want := slices.Sorted(slices.Values(world.HookEvents))
	if !slices.Equal(got, want) {
		t.Fatalf("AppSpecHookTypes names %v, kiln admits %v: a hook that fires in the preview must fire in the generated app", got, want)
	}
}
