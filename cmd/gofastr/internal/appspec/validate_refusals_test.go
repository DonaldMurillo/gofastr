package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

const refusalEntities = `entities:
  - name: notes
    fields:
      - name: title
        type: string
        required: true
      - name: secret
        type: string
        hidden: true
      - name: size
        type: int
  - name: tags
    crud: false
    fields:
      - name: label
        type: string
`

func refusalSpec(app, entities, rest string) string {
	return "app:\n  name: a\n  module: example.com/a\n" + app + refusalEntities + entities + rest
}

func refusalScreen(block string) string {
	return "screens:\n  - name: page\n    route: /page/{id}\n    body:\n" + block
}

// Each case is a spec ValidateAppSpec refuses, with a phrase its error holds.
func TestValidateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"pwa start_url is root-relative", refusalSpec("  pwa:\n    enabled: true\n    start_url: home\n", "", ""), "app.pwa.start_url"},
		{"pwa scope is root-relative", refusalSpec("  pwa:\n    enabled: true\n    scope: app\n", "", ""), "app.pwa.scope"},
		{"theme value grammar", refusalSpec("  theme:\n    primary: \"red;}\"\n", "", ""), "app.theme primary"},
		{"dark theme token", refusalSpec("  theme:\n    dark:\n      bogus: red\n", "", ""), "unsupported color token"},
		{"dark theme value grammar", refusalSpec("  theme:\n    dark:\n      primary: \"red;}\"\n", "", ""), "app.theme.dark primary"},
		{"table is an identifier", refusalSpec("", "  - name: logs\n    table: bad-name\n    fields:\n      - name: line\n        type: string\n", ""), "(it becomes a SQL identifier"},
		{"enum value has no invisible rune", refusalSpec("", "  - name: logs\n    fields:\n      - name: level\n        type: enum\n        values: [\"ok\", \"a\u200bb\"]\n", ""), "C1 control or invisible"},
		{"rename to itself", refusalSpec("", "  - name: logs\n    renames:\n      line: line\n    fields:\n      - name: line\n        type: string\n", ""), "to itself"},
		{"rename keeps the old field", refusalSpec("", "  - name: logs\n    renames:\n      line: text\n    fields:\n      - name: line\n        type: string\n      - name: text\n        type: string\n", ""), "while still declaring"},
		{"relation has a name", refusalSpec("", "  - name: logs\n    relations:\n      - entity: notes\n    fields:\n      - name: line\n        type: string\n", ""), "relation with no name"},
		{"relation name is an identifier", refusalSpec("", "  - name: logs\n    relations:\n      - name: 2nd\n        entity: notes\n    fields:\n      - name: line\n        type: string\n", ""), "does not produce a valid Go identifier: it is emitted as a struct field"},
		{"relation name is not a field", refusalSpec("", "  - name: logs\n    relations:\n      - name: line\n        entity: notes\n    fields:\n      - name: line\n        type: string\n", ""), "collides with field line"},
		{"layout is known", refusalSpec("", "", "screens:\n  - name: home\n    route: /\n    layout: markting\n"), "is not a layout"},
		{"composite key param", refusalSpec("", "  - name: items\n    primary_key: [org, code]\n    fields:\n      - name: org\n        type: string\n        required: true\n      - name: code\n        type: string\n        required: true\n", "screens:\n  - name: item\n    route: /items/{code}/{id}\n    body:\n      - kind: entity_detail\n        entity: items\n"), "is a key column of the record it shows"},
		{"hook handler is set", refusalSpec("", "", "hooks:\n  - id: h\n    entity: notes\n    when: before_create\n"), "handler is required"},
		{"hook handlers differ", refusalSpec("", "", "hooks:\n  - id: a\n    entity: notes\n    when: before_create\n    handler: Check\n  - id: b\n    entity: notes\n    when: after_create\n    handler: Check\n"), "duplicate hook handler"},
		{"nav icon has no markup", refusalSpec("", "", "nav:\n  - label: Home\n    href: /\n    icon: \"<b>x</b>\"\n"), "contains markup"},
		{"nested nav icon has no markup", refusalSpec("", "", "nav:\n  - label: Home\n    href: /\n    items:\n      - label: Child\n        href: /child\n        icon: \"<b>x</b>\"\n"), "nav item \"Child\" icon"},
		{"enum value has no C1 control", refusalSpec("", "  - name: logs\n    fields:\n      - name: level\n        type: enum\n        values: [\"ok\", \"a\\u0085b\"]\n", ""), "C1 control or invisible"},
		{"one list per entity at any depth", refusalSpec("", "", refusalScreen("      - kind: entity_list\n        entity: notes\n      - kind: section\n        props:\n          heading: More\n        children:\n          - kind: entity_list\n            entity: notes\n")), "lists entity \"notes\" more than once"},
		{"filter only on a list", refusalSpec("", "", refusalScreen("      - type: paragraph\n        text: hi\n        filter: a=b\n")), "filter applies only to an entity_list"},
		{"list field not hidden", refusalSpec("", "", refusalScreen("      - kind: entity_list\n        entity: notes\n        fields: [secret]\n")), "is hidden on entity"},
		{"list field not omitted", refusalSpec("", "  - name: logs\n    display:\n      fields:\n        line:\n          omit: true\n    fields:\n      - name: line\n        type: string\n", refusalScreen("      - kind: entity_list\n        entity: logs\n        fields: [line]\n")), "omitted by entity"},
		{"list field once", refusalSpec("", "", refusalScreen("      - kind: entity_list\n        entity: notes\n        fields: [title, title]\n")), "is listed twice"},
		{"form names an entity", refusalSpec("", "", refusalScreen("      - kind: entity_form\n")), "entity_form block entity is required"},
		{"form entity is declared", refusalSpec("", "", refusalScreen("      - kind: entity_form\n        entity: nope\n")), "entity_form targets unknown entity"},
		{"form entity has crud", refusalSpec("", "", refusalScreen("      - kind: entity_form\n        entity: tags\n")), "entity_form target \"tags\" must enable crud"},
		{"form mode is create or edit", refusalSpec("", "", refusalScreen("      - kind: entity_form\n        entity: notes\n        mode: update\n")), "entity_form mode must be"},
		{"detail names an entity", refusalSpec("", "", refusalScreen("      - kind: entity_detail\n")), "entity_detail block entity is required"},
		{"detail entity is declared", refusalSpec("", "", refusalScreen("      - kind: entity_detail\n        entity: nope\n")), "entity_detail targets unknown entity"},
		{"detail entity has crud", refusalSpec("", "", refusalScreen("      - kind: entity_detail\n        entity: tags\n")), "entity_detail target \"tags\" must enable crud"},
		{"detail takes no text", refusalSpec("", "", refusalScreen("      - kind: entity_detail\n        entity: notes\n        text: Note\n")), "takes no text or fields"},
		{"auth action is a string", refusalSpec("", "", refusalScreen("      - kind: login_form\n        props:\n          action: 5\n")), "action must be a string"},
		{"auth action is safe", refusalSpec("", "", refusalScreen("      - kind: login_form\n        props:\n          action: \"javascript:alert(1)\"\n")), "is not a safe form action"},
		{"chart source is complete", refusalSpec("", "", refusalScreen("      - kind: bar_chart\n        props:\n          source:\n            entity: notes\n")), "needs both entity and group_by"},
		{"chart entity is declared", refusalSpec("", "", refusalScreen("      - kind: bar_chart\n        props:\n          source:\n            entity: nope\n            group_by: title\n")), "source targets unknown entity"},
		{"stat card source names an entity", refusalSpec("", "", refusalScreen("      - kind: stat_card\n        props:\n          label: Notes\n          source:\n            agg: count\n")), "stat_card source needs entity"},
		{"stat card entity is declared", refusalSpec("", "", refusalScreen("      - kind: stat_card\n        props:\n          label: Notes\n          source:\n            entity: nope\n")), "stat_card source targets unknown entity"},
		{"stat card entity has crud", refusalSpec("", "", refusalScreen("      - kind: stat_card\n        props:\n          label: Notes\n          source:\n            entity: tags\n")), "stat_card source entity \"tags\" must enable crud"},
		{"options are a list", refusalSpec("", "", refusalScreen("      - kind: select_field\n        props:\n          label: L\n          name: n\n          options: x\n")), "options must be a list"},
		{"option label is text", refusalSpec("", "", refusalScreen("      - kind: select_field\n        props:\n          label: L\n          name: n\n          options:\n            - label: 5\n")), "options[0].label must be a string"},
		{"option is text or a map", refusalSpec("", "", refusalScreen("      - kind: select_field\n        props:\n          label: L\n          name: n\n          options: [5]\n")), "must be a string or a {label, value} map"},
		{"option has a label", refusalSpec("", "", refusalScreen("      - kind: select_field\n        props:\n          label: L\n          name: n\n          options: [\" \"]\n")), "options[0] needs a label"},
		{"table columns are a list", refusalSpec("", "", refusalScreen("      - kind: data_table\n        props:\n          columns: a\n")), "columns must be a list"},
		{"table column is text", refusalSpec("", "", refusalScreen("      - kind: data_table\n        props:\n          columns: [1]\n")), "columns[0] must be a string"},
		{"table rows are lists", refusalSpec("", "", refusalScreen("      - kind: data_table\n        props:\n          columns: [a]\n          rows: x\n")), "rows must be a list of lists"},
		{"layout prop is text", refusalSpec("", "", refusalScreen("      - kind: stack\n        props:\n          gap: 2\n")), "prop \"gap\" must be a string"},
		{"stack align is supported", refusalSpec("", "", refusalScreen("      - kind: stack\n        props:\n          align: middle\n")), "align \"middle\" is unsupported"},
		{"stack justify is supported", refusalSpec("", "", refusalScreen("      - kind: stack\n        props:\n          justify: spread\n")), "justify \"spread\" is unsupported"},
		{"cluster no_wrap is a bool", refusalSpec("", "", refusalScreen("      - kind: cluster\n        props:\n          no_wrap: \"yes\"\n")), "no_wrap\" must be a boolean"},
		{"chart groups a declared column", refusalSpec("", "", refusalScreen("      - kind: bar_chart\n        props:\n          source:\n            entity: notes\n            group_by: colour\n")), "group_by \"colour\" is not defined on entity"},
		{"table row is a list of text", refusalSpec("", "", refusalScreen("      - kind: data_table\n        props:\n          columns: [a]\n          rows: [x]\n")), "rows[0]:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := decodeSpecString(t, tc.src)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			err = ValidateAppSpec(bp)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateAppSpec = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestStaticDirReadsBackslashesAsSlashes(t *testing.T) {
	if staticDirProblem(`..\`, "", "") == "" {
		t.Fatal(`static_dir ..\ is the directory above the app and must be refused`)
	}
	if why := staticDirProblem("/srv/static", "", ""); why != "" {
		t.Fatalf("an absolute static_dir is the deployer's to place, got %q", why)
	}
}

// The decoder refuses a repeated name too; a spec built in Go never passed it.
func TestEndpointNamesDifferInAGoBuiltSpec(t *testing.T) {
	e := settings.Endpoint{Name: "publish", Entity: "notes", Method: "POST", Path: "{id}/publish", Handler: "entities/notes_endpoints.go#Publish"}
	again := e
	again.Path, again.Handler = "{id}/archive", "entities/notes_endpoints.go#Archive"
	bp := AppSpec{Entities: []framework.EntityDeclaration{{Name: "notes", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}}, Endpoints: []settings.Endpoint{e, again}}
	if err := ValidateAppSpecEndpoints(bp); err == nil || !strings.Contains(err.Error(), "two endpoints are named publish") {
		t.Fatalf("ValidateAppSpecEndpoints = %v, want the repeated name refused", err)
	}
}

// entity_create and entity_edit cannot be decoded, so only a spec built in Go can hold them.
func TestValidateRefusesRetiredEntityKinds(t *testing.T) {
	for kind, want := range map[string]string{"entity_create": "uses entity_create", "entity_edit": "uses entity_edit"} {
		bp := AppSpec{Screens: []AppSpecScreen{{Name: "page", Route: "/page", Body: []AppSpecBlock{{Kind: kind, Entity: "notes"}}}}}
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: ValidateAppSpec = %v, want an error containing %q", kind, err, want)
		}
	}
}
