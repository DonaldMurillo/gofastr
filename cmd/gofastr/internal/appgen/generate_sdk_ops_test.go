package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// sdkReadmeFixture builds a one-entity SDK spec whose only field f is
// of declType, the probe for the readme example pickers.
func sdkReadmeFixture(declType string) sdkSpec {
	decl := framework.EntityDeclaration{
		Name:   "things",
		Table:  "things",
		Fields: []framework.FieldDeclaration{{Name: "f", Type: declType}},
	}
	return sdkSpec{
		App: "things", SDKVersion: "test", GofastrVersion: "test",
		BaseURL: "https://example.test", Decls: []framework.EntityDeclaration{decl},
		Entities: []cliEntity{buildEntityModel(decl, nil)},
	}
}

// Both SDK readmes' canonical example offers `<f>_gte` exactly when
// the column's type accepts a range comparison (filter.OpSuitsType,
// via the CLI model's derived operator set): a `_gte` on a Bool or
// JSON column is a request the server answers 400, printed by the
// snippet whose whole job is demonstrating the contract. Both
// directions, over every field type.
func TestSDKReadmeExamplesMatchOpMatrix(t *testing.T) {
	for _, c := range []struct {
		declType string
		gteOK    bool
	}{
		{"string", true}, {"text", true}, {"int", true}, {"float", true},
		{"decimal", true}, {"bool", false}, {"enum", true}, {"uuid", true},
		{"timestamp", true}, {"date", true}, {"json", false},
	} {
		goReadme := renderSDKGoReadme(sdkReadmeFixture(c.declType))
		jsReadme := renderSDKJSReadme(sdkReadmeFixture(c.declType))
		goHas := strings.Contains(goReadme, `"f_gte"`)
		jsHas := strings.Contains(jsReadme, `+ "_gte"`)
		if goHas != c.gteOK {
			t.Errorf("type %s: Go readme offers f_gte = %v, want %v", c.declType, goHas, c.gteOK)
		}
		if jsHas != c.gteOK {
			t.Errorf("type %s: JS readme offers _gte = %v, want %v", c.declType, jsHas, c.gteOK)
		}
		// A JSON column takes no filter at all (D17), so its readme shows
		// none; every other type keeps the equality line.
		if got, want := strings.Contains(goReadme, `params.Set("f"`), c.declType != "json"; got != want {
			t.Errorf("type %s: Go readme shows the equality example = %v, want %v", c.declType, got, want)
		}
	}
}

// The generated CLI's list verb offers no flag for a JSON column (D17):
// the server refuses every filter on one, the bare equality included.
func TestCLIListOmitsJSONFilterFlag(t *testing.T) {
	spec := sdkReadmeFixture("json")
	var sb strings.Builder
	renderCLIListTables(&sb, spec.Entities[0])
	if strings.Contains(sb.String(), `param: "f"`) {
		t.Errorf("the list table offers a filter flag for a JSON column:\n%s", sb.String())
	}
}
