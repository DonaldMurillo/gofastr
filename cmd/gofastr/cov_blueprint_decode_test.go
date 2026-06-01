package main

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// covT_decode parses a YAML blueprint body and runs the full decoder.
func covT_decode(t *testing.T, yaml string) (Blueprint, error) {
	t.Helper()
	node, err := coreyaml.Parse(yaml)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, yaml)
	}
	return decodeBlueprint(node)
}

func covT_wantDecodeErr(t *testing.T, yaml string) {
	t.Helper()
	if _, err := covT_decode(t, yaml); err == nil {
		t.Fatalf("expected decode error for:\n%s", yaml)
	}
}

func TestDecodeBlueprintErrorBranches(t *testing.T) {
	// Top-level unknown key.
	covT_wantDecodeErr(t, "bogus_key: 1\n")

	// app.* unknown key + wrong db type + theme non-scalar + bad theme token.
	covT_wantDecodeErr(t, "app:\n  zzz: 1\n")
	covT_wantDecodeErr(t, "app:\n  db: not_a_map\n")
	covT_wantDecodeErr(t, "app:\n  db:\n    bad: 1\n")
	covT_wantDecodeErr(t, "app:\n  theme:\n    primary:\n      nested: 1\n")

	// entities: must be a list; entity unknown key; nested field/relation/index/endpoint errors.
	covT_wantDecodeErr(t, "entities: not_a_list\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    bogus: 1\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    fields: not_a_list\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    fields:\n      - name: f\n        bad: 1\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    relations: not_a_list\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    relations:\n      - type: bogus_rel\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    relations:\n      - bad: 1\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    indices: not_a_list\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    indices:\n      - bad: 1\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    endpoints: not_a_list\n")
	covT_wantDecodeErr(t, "entities:\n  - name: x\n    endpoints:\n      - bad: 1\n")

	// screens: bad list / unknown key / bad body / nested children + actions.
	covT_wantDecodeErr(t, "screens: not_a_list\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    bogus: 1\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    body: not_a_list\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    body:\n      - type: p\n        bad: 1\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    body:\n      - type: p\n        children: not_a_list\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    body:\n      - type: p\n        actions: not_a_list\n")
	covT_wantDecodeErr(t, "screens:\n  - name: s\n    body:\n      - type: p\n        actions:\n          - bad: 1\n")

	// top-level endpoints: bad list / unknown key.
	covT_wantDecodeErr(t, "endpoints: not_a_list\n")
	covT_wantDecodeErr(t, "endpoints:\n  - name: e\n    bad: 1\n")

	// named stubs (middleware/plugins/helpers): bad list / unknown key.
	covT_wantDecodeErr(t, "middleware: not_a_list\n")
	covT_wantDecodeErr(t, "plugins:\n  - bad: 1\n")
	covT_wantDecodeErr(t, "helpers:\n  - bad: 1\n")
}

func TestDecodeBlueprintHappyFullShape(t *testing.T) {
	yaml := `
app:
  name: Demo
  module: example.com/demo
  static_dir: public
  output_dir: gen
  db:
    driver: sqlite
    url: file:demo.db
  theme:
    primary: "#fff"
entities:
  - name: users
    table: users
    soft_delete: true
    multi_tenant: true
    mcp: true
    timestamps: true
    crud: true
    cursor_field: id
    cursor_fields: [a, b]
    properties:
      key: value
    fields:
      - name: email
        type: string
        required: true
        unique: true
        max: 100
        min: 1
        pattern: ".+"
        values: [x, y]
        read_only: true
        hidden: true
    relations:
      - type: has_many
        name: posts
        entity: post
        foreign_key: user_id
    indices:
      - name: ix_email
        columns: [email]
        unique: true
    endpoints:
      - method: GET
        path: /ping
        name: ping
        handler: PingHandler
screens:
  - name: home
    route: /
    title: Home
    type: page
    body:
      - type: heading
        level: 1
        text: Hi
        actions:
          - name: save
            event: click
            client_js: "console.log(1)"
        children:
          - type: p
            text: child
endpoints:
  - name: health
    method: GET
    path: /health
    entity: ""
middleware:
  - logging
  - name: auth
    description: requires login
plugins:
  - name: metrics
helpers:
  - name: fmt
`
	bp, err := covT_decode(t, yaml)
	if err != nil {
		t.Fatalf("decode happy: %v", err)
	}
	if len(bp.Entities) != 1 || len(bp.Screens) != 1 {
		t.Fatalf("unexpected shape: %+v", bp)
	}
	if len(bp.Middleware) != 2 || bp.Middleware[0].Name != "logging" {
		t.Fatalf("middleware: %+v", bp.Middleware)
	}
}

func TestValidateBlueprintBadThemeToken(t *testing.T) {
	bp := Blueprint{App: BlueprintApp{Theme: map[string]string{"not_a_real_token": "#fff"}}}
	if err := validateBlueprint(bp); err == nil {
		t.Fatal("expected unsupported theme token error")
	}
}
