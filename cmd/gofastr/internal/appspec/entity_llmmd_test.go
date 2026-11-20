package appspec

import (
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestDecodeEntityLLMMD(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("value="+value, func(t *testing.T) {
			src := "app:\n  name: Demo\n"
			if value != "" {
				src += "  entity_llm_md: " + value + "\n"
			}
			node, err := coreyaml.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			spec, err := DecodeAppSpec(node)
			if err != nil {
				t.Fatal(err)
			}
			field := spec.App.EntityLLMMD
			if value == "" {
				if field != nil {
					t.Fatal("omitted setting must stay nil")
				}
			} else if field == nil || *field != (value == "true") {
				t.Fatalf("entity_llm_md did not decode %s", value)
			}
		})
	}
}

func TestEntityLLMMDFreesIndexScreenRoute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting *bool
		refused bool
	}{{"omitted", nil, true}, {"true", new(true), true}, {"false", new(false), false}} {
		bp := AppSpec{App: AppSpecApp{Name: "Demo", EntityLLMMD: tc.setting},
			Entities: []framework.EntityDeclaration{{Name: "tickets", Exposure: &framework.ExposureDeclaration{CRUD: new(true)}, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
			Screens:  []AppSpecScreen{{Name: "index", Route: "/api/llm.md", Body: []AppSpecBlock{{Type: "heading", Text: "Index"}}}},
		}
		err := ValidateAppSpec(bp)
		if refused := err != nil && strings.Contains(err.Error(), "framework serves"); refused != tc.refused {
			t.Errorf("entity_llm_md %s: validate = %v; want refused=%v", tc.name, err, tc.refused)
		}
	}
}
