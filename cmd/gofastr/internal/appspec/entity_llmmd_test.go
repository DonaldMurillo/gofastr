package appspec

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
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
