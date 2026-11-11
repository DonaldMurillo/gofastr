package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A list's create: true synthesizes the create screen at
// <list>/create, named <entity>_create. An authored screen at that
// route took it over (New opened a page with no form) and one named
// <entity>_create generated the synthesized screen's type a second time.
func TestAuthoredCreateScreenRefused(t *testing.T) {
	const head = `app:
  name: Shop
  module: example.com/shop
entities:
  - name: orders
    crud: true
    fields:
      - name: label
        type: string
screens:
  - name: orders
    route: /orders
    body:
      - kind: entity_list
        entity: orders
        fields: [label]
`
	const page = `    body:
      - type: heading
        level: 1
        text: Mine
`
	for _, tc := range []struct {
		name, create, extra, want string
	}{
		{"route taken", "        create: true\n", "  - name: create_page\n    route: /orders/create\n" + page, "/orders/create"},
		{"trailing slash", "        create: true\n", "  - name: create_page\n    route: /orders/create/\n" + page, "/orders/create"},
		{"name taken", "        create: true\n", "  - name: orders_create\n    route: /new-order\n" + page, "orders_create"},
		{"no create", "", "  - name: create_page\n    route: /orders/create\n" + page, ""},
		{"other route", "        create: true\n", "  - name: create_page\n    route: /orders/new\n" + page, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gofastr.yml")
			clitest.WriteTestFile(t, path, head+tc.create+tc.extra)
			_, err := LoadAppSpec(path)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("an authored screen in the synthesized create screen's place must be refused")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "create") {
				t.Fatalf("error should name %s, got: %v", tc.want, err)
			}
		})
	}
}
