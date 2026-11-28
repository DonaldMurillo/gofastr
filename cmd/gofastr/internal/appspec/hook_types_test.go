package appspec

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestHookTypesMatchKiln(t *testing.T) {
	got := slices.Sorted(maps.Keys(AppSpecHookTypes))
	want := slices.Sorted(slices.Values(world.HookEvents))
	if !slices.Equal(got, want) {
		t.Fatalf("AppSpecHookTypes names %v, kiln admits %v: a hook that fires in the preview must fire in the generated app", got, want)
	}
}

// Each when spelling names the constant whose own String is that spelling without underscores.
func TestHookTypesNameTheirConstant(t *testing.T) {
	constants := map[string]framework.HookType{
		"framework.BeforeCreate": framework.BeforeCreate,
		"framework.AfterCreate":  framework.AfterCreate,
		"framework.BeforeUpdate": framework.BeforeUpdate,
		"framework.AfterUpdate":  framework.AfterUpdate,
		"framework.BeforeDelete": framework.BeforeDelete,
		"framework.AfterDelete":  framework.AfterDelete,
		"framework.BeforeList":   framework.BeforeList,
		"framework.AfterList":    framework.AfterList,
	}
	for when, name := range AppSpecHookTypes {
		c, ok := constants[name]
		if !ok {
			t.Errorf("AppSpecHookTypes[%q] = %q, not a framework hook constant", when, name)
			continue
		}
		if want := strings.ReplaceAll(when, "_", ""); c.String() != want {
			t.Errorf("AppSpecHookTypes[%q] = %s, which fires on %s", when, name, c.String())
		}
	}
}
