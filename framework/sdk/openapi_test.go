package sdk

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A manifest from an OpenAPI document lists no entities; one from the
// entity set lists some, and a source the reader does not know is no
// manifest at all.
func TestReadManifestSources(t *testing.T) {
	const art = `"artifacts":{"go":{"file":"sdk-go.zip","sha256":"ab"}}`
	for name, tc := range map[string]struct {
		raw string
		ok  bool
	}{
		"openapi":               {`{"schemaVersion":2,"source":"openapi","entities":[],` + art + `}`, true},
		"openapi null entities": {`{"schemaVersion":2,"source":"openapi","entities":null,` + art + `}`, true},
		"openapi with entities": {`{"schemaVersion":2,"source":"openapi","entities":["posts"],` + art + `}`, false},
		"entities":              {`{"schemaVersion":2,"entities":["posts"],` + art + `}`, true},
		"entities missing":      {`{"schemaVersion":2,"entities":[],` + art + `}`, false},
		"unknown source":        {`{"schemaVersion":2,"source":"graphql","entities":[],` + art + `}`, false},
	} {
		m, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: []byte(tc.raw)}})
		if (err == nil) != tc.ok {
			t.Errorf("%s: err %v, want ok=%v", name, err, tc.ok)
		}
		if err == nil && strings.Contains(tc.raw, "openapi") && m.Source != SourceOpenAPI {
			t.Errorf("%s: source %q", name, m.Source)
		}
	}
}

// The OpenAPI hash reads the document's canonical JSON: key order and
// whitespace never move it, a changed value does, and bytes that are not
// one JSON value are an error.
func TestOpenAPIHashIsCanonical(t *testing.T) {
	a, err := OpenAPIHash([]byte(`{"openapi":"3.1.0","paths":{"/a":{"get":{"operationId":"a"}}},"n":1.0}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenAPIHash([]byte("{\n  \"paths\": {\"/a\": {\"get\": {\"operationId\": \"a\"}}},\n  \"n\": 1,\n  \"openapi\": \"3.1.0\"\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a != b || !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("reformatting moved the hash: %s vs %s", a, b)
	}
	c, err := OpenAPIHash([]byte(`{"openapi":"3.1.0","paths":{"/a":{"get":{"operationId":"b"}}},"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("a renamed operation kept the hash")
	}
	for _, bad := range []string{``, `{`, `{"a":1}{}`, `{"a":1,"a":2}`} {
		if _, err := OpenAPIHash([]byte(bad)); err == nil {
			t.Errorf("%q hashed", bad)
		}
	}
}
