package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Property family: the app spec's strict-decoding contracts must hold at
// EVERY decode surface, not just the YAML one.
//
// decodeAppSpecFile accepts .json app specs and parses them with
// encoding/json (yamlNodeFromJSON), a parser with silent last-wins duplicate
// keys. The YAML leg rejects duplicates at the parser (core/yaml
// security_test.go pins it with this exact rationale: "silent last-wins lets
// a stale or hostile copy-paste line override the value a reviewer believes
// is in force ... for this parser's consumers — gofastr app specs ...").
// The .json leg of the same decoder quietly took the last copy: a reviewer
// (or an agent merging two app spec fragments into JSON) reads the first
// `enabled: false` while the app generates from the second.
//
// Threat model is the sibling files' (appspec_injection,
// emitter_quoting): an app spec is developer-authored OR agent-transcribed
// text, and merges are exactly where duplicates come from.

// decodeJSONAppSpec writes body to a temp .json file and runs the real
// file decoder over it.
func decodeJSONAppSpec(t *testing.T, body string) (AppSpec, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return decodeAppSpecFile(path)
}

// TestJSONAppSpecRejectsDuplicateKeys: a duplicate mapping key anywhere in
// a .json app spec must be an error. Surfaces: the root object, an app
// section key, an entity object's key, a field object's key, and a seed row.
func TestJSONAppSpecRejectsDuplicateKeys(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			"duplicate root section",
			`{"app": {"name": "a"}, "app": {"name": "b"}}`,
		},
		{
			"duplicate app key",
			`{"app": {"name": "a", "name": "b"}}`,
		},
		{
			"duplicate entity key",
			`{"entities": [{"name": "x", "name": "y", "fields": []}]}`,
		},
		{
			"duplicate field key",
			`{"entities": [{"name": "x", "fields": [{"name": "f", "name": "g"}]}]}`,
		},
		{
			"duplicate seed row key",
			`{"entities": [{"name": "x", "fields": [{"name": "title"}]}], "seed": [{"entity": "x", "rows": [{"title": "a", "title": "b"}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeJSONAppSpec(t, tc.body)
			if err == nil {
				t.Fatalf("SECURITY: [decode] .json app spec accepted duplicate keys (silent last-wins): %s", tc.body)
			}
		})
	}
}

// TestJSONAppSpecStillParses is the control for the rejection above: a
// well-formed .json app spec with every section shape decodes, and the
// strict unknown-key contract the YAML leg carries holds here too. Guards
// against a fix that achieves rejection by refusing .json entirely.
func TestJSONAppSpecStillParses(t *testing.T) {
	bp, err := decodeJSONAppSpec(t, `{
		"app": {"name": "JsonApp", "module": "example.com/jsonapp", "db": {"driver": "sqlite", "url": "file:j.db"}},
		"entities": [{"name": "posts", "fields": [{"name": "title", "type": "string"}]}],
		"seed": [{"entity": "posts", "rows": [{"title": "hello"}]}]
	}`)
	if err != nil {
		t.Fatalf("well-formed JSON app spec must decode: %v", err)
	}
	if bp.App.Name != "JsonApp" || len(bp.Entities) != 1 || len(bp.Seed) != 1 || len(bp.Seed[0].Rows) != 1 {
		t.Fatalf("JSON app spec decoded wrong: %+v", bp)
	}
	if bp.Seed[0].Rows[0]["title"] != "hello" {
		t.Fatalf("seed row value lost: %+v", bp.Seed[0].Rows)
	}
	if _, err := decodeJSONAppSpec(t, `{"bogus_key": 1}`); err == nil {
		t.Fatal("unknown-key strictness must hold on the JSON leg too")
	}
}

// Property: a value the decoder cannot read must be an error, not a silent
// zero-default — specifically for app.db, where the silent default is "no
// database".
//
// app.db.driver and app.db.url decode through stringValue, which returns ""
// for a non-scalar node. A driver written as a nested map (an agent merging
// fragments, or a YAML anchor gone wrong) therefore decodes to "" and the
// generated openDB takes its `driver == "" && dbURL == ""` branch: the app
// ships with NO database, silently — every CRUD screen 500s at runtime and,
// per the seed fail-fast contract, any seed data aborts boot. The theme
// decoder already built requireScalarString for exactly this shape
// ("app.theme primary: non-scalar" is a decode error); db is the sharper
// surface and lacks the guard.
func TestDBConfigRejectsNonScalarTypes(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"driver as map", "app:\n  name: X\n  db:\n    driver:\n      nested: true\n"},
		{"driver as list", "app:\n  name: X\n  db:\n    driver: [sqlite]\n"},
		{"url as map", "app:\n  name: X\n  db:\n    url:\n      host: h\n"},
		{"url as list", "app:\n  name: X\n  db:\n    url: [a, b]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeAppSpecString(tc.yaml)
			if err == nil {
				t.Fatalf("SECURITY: [decode] %s decoded to the empty string; the app silently generates with no database:\n%s", tc.name, tc.yaml)
			}
		})
	}
	// Control: scalar driver/url still decode.
	bp, err := decodeAppSpecString("app:\n  name: X\n  db:\n    driver: sqlite\n    url: file:x.db\n")
	if err != nil {
		t.Fatalf("scalar db config must decode: %v", err)
	}
	if bp.App.DBDriver != "sqlite" || bp.App.DBURL != "file:x.db" {
		t.Fatalf("scalar db config lost: %+v", bp.App)
	}
}
