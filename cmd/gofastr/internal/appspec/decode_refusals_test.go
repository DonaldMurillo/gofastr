package appspec

import (
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

func decodeSpecString(t *testing.T, src string) (AppSpec, error) {
	t.Helper()
	node, err := coreyaml.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return DecodeAppSpec(node)
}

// Each case is a decode the app spec refuses, with a phrase its error holds.
func TestDecodeRefusals(t *testing.T) {
	const entity = "app:\n  name: a\nentities:\n  - name: notes\n    fields:\n      - name: title\n        type: string\n      - name: secret\n        type: string\n        hidden: true\n      - name: size\n        type: int\n"
	for _, tc := range []struct {
		name, src, want string
	}{
		{"entity_create is no block kind", "screens:\n  - name: home\n    route: /\n    body:\n      - kind: entity_create\n        entity: notes\n", "entity_create is no longer a block kind"},
		{"entity_edit is no block kind", "screens:\n  - name: home\n    route: /\n    body:\n      - kind: entity_edit\n        entity: notes\n", "entity_edit is no longer a block kind"},
		{"dark theme value is a scalar", "app:\n  name: a\n  theme:\n    dark:\n      primary: [red]\n", "app.theme.dark.primary must be a scalar"},
		{"cross_owner_read needs owner_field", entity + "    cross_owner_read: notes:read_all\n", "has no owner_field"},
		{"search on a hidden field", entity + "    search_fields: [secret]\n", "search_fields entry \"secret\" is Hidden"},
		{"search on a non-text field", entity + "    search_fields: [size]\n", "must be string or text"},
		{"cursor on a hidden field", entity + "    cursor_field: secret\n", "cursor field \"secret\" is Hidden"},
		{"cursor on a no_query field", "app:\n  name: a\nentities:\n  - name: notes\n    cursor_field: code\n    fields:\n      - name: code\n        type: string\n        required: true\n        no_query: true\n", "cursor field \"code\" is no_query"},
		{"display key the entity package refuses", entity + "    display:\n      bogus: 1\n", "entities[0].display"},
		{"seed count as a string", entity + "seed:\n  - entity: notes\n    count: \"5\"\n", "must be a whole number"},
		{"seed count with a fraction", entity + "seed:\n  - entity: notes\n    count: 2.5\n", "must be a whole number"},
		{"seed weight as a string", entity + "seed:\n  - entity: notes\n    count: 2\n    weights:\n      title:\n        a: \"5\"\n", "must be a whole number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeSpecString(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestAPIPrefixDefaultsToAPI(t *testing.T) {
	for src, want := range map[string]string{
		"app:\n  name: a\n":                     "api",
		"app:\n  name: a\n  api_prefix: /v1/\n": "v1",
		"app:\n  name: a\n  api_prefix: \"\"\n": "",
	} {
		bp, err := decodeSpecString(t, src)
		if err != nil {
			t.Fatal(err)
		}
		if bp.App.APIPrefix != want {
			t.Errorf("%q: APIPrefix = %q, want %q", src, bp.App.APIPrefix, want)
		}
	}
}

func TestUnknownKeysReportedSorted(t *testing.T) {
	_, err := decodeSpecString(t, "zeta: 1\neta: 1\ndelta: 1\nbeta: 1\nalpha: 1\ngamma: 1\n")
	if err == nil {
		t.Fatal("unknown root keys decoded")
	}
	msg, last := err.Error(), -1
	for _, key := range []string{"alpha", "beta", "delta", "eta", "gamma", "zeta"} {
		i := strings.Index(msg, `"`+key+`"`)
		if i < last {
			t.Fatalf("unknown keys not reported in sorted order: %s", msg)
		}
		last = i
	}
}
