package appspec

import (
	"strings"
	"testing"
)

// A field declared twice in one entity passed validation and surfaced
// from the render pass as "generated code declares Products.Title twice
// (entities/client/client.go and entities/client/client.go)": a file the
// author never wrote, named twice. Validation names the entity and field.
func TestValidateRefusesDuplicateField(t *testing.T) {
	dir := writeSpecDir(t, map[string]string{
		"gofastr.yml": `app:
  name: Shop
  module: example.com/shop
entities:
  - name: products
    fields:
      - name: title
        type: string
      - name: title
        type: text
`,
	})
	_, err := LoadAppSpec(dir)
	if err == nil {
		t.Fatal("LoadAppSpec accepted an entity that declares title twice")
	}
	if !strings.Contains(err.Error(), `entity "products" declares field "title" twice`) {
		t.Fatalf("error does not name the duplicate field: %v", err)
	}
}
