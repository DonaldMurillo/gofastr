package appgen

import (
	"strings"
	"testing"
)

// A JSON body property is wire data the operator cannot rename, and
// --json sends any property, so one whose name derives no usable flag
// (@type), a flag the CLI reserves (url, token, file, json), or a flag a
// parameter already holds gets no flag of its own rather than failing the
// whole document. A webhook's {"url": ...} body is the common case.
func TestOpenAPIBodyFieldWithoutFlagUsesJSON(t *testing.T) {
	doc := mustDecodeDoc(t, `{"openapi":"3.0.3","paths":{"/posts/{postId}/hooks":{"post":{
	  "operationId":"createHook",
	  "parameters":[
	    {"name":"postId","in":"path","required":true,"schema":{"type":"string"}},
	    {"name":"mode","in":"query","schema":{"type":"string"}}],
	  "requestBody":{"required":true,"content":{"application/json":{"schema":{
	    "type":"object","required":["url"],
	    "properties":{
	      "url":{"type":"string"},"token":{"type":"string"},"file":{"type":"string"},
	      "json":{"type":"string"},"@type":{"type":"string"},
	      "post_id":{"type":"string"},"mode":{"type":"string"},
	      "name":{"type":"string"}}}}}}}}}}`)
	spec, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
	if err != nil {
		t.Fatalf("a body property the CLI cannot flag must not fail generation: %v", err)
	}
	var flags []string
	for _, f := range spec.Ops[0].BodyFields {
		flags = append(flags, f.Flag)
	}
	if got := strings.Join(flags, ","); got != "name" {
		t.Errorf("body field flags = %q, want only name", got)
	}
	src := renderCLIOpsFile(spec)
	if !strings.Contains(src, `jsonF := fs.String("json"`) {
		t.Errorf("the operation lost its --json escape:\n%s", src)
	}
	// Two properties that derive the same flag: the first by name keeps it.
	doc = mustDecodeDoc(t, `{"openapi":"3.0.3","paths":{"/a":{"post":{"operationId":"mk",
	  "requestBody":{"content":{"application/json":{"schema":{"type":"object",
	    "properties":{"user_id":{"type":"string"},"userId":{"type":"integer"}}}}}}}}}}`)
	spec, err = BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
	if err != nil {
		t.Fatalf("colliding body properties must not fail generation: %v", err)
	}
	if f := spec.Ops[0].BodyFields; len(f) != 1 || f[0].Wire != "userId" || f[0].Flag != "user-id" {
		t.Errorf("body fields = %+v, want userId alone on --user-id", f)
	}
}

// A binary body names its schema however the document likes: a $ref to a
// component {type: string, format: binary} is the same binary body as the
// schema written inline. A multipart or form-urlencoded body is a set of
// encoded fields, never a file sent as is, so it fails generation even
// when its schema says format: binary: sent raw, a multipart body has no
// boundary and the server cannot read it.
func TestOpenAPIBinaryBodySchema(t *testing.T) {
	body := func(ct, schema string) map[string]any {
		return mustDecodeDoc(t, `{"openapi":"3.0.3",
		  "components":{"schemas":{"Upload":{"type":"string","format":"binary"}}},
		  "paths":{"/up":{"post":{"operationId":"up","requestBody":{"content":{`+
			`"`+ct+`":{"schema":`+schema+`}}}}}}}`)
	}
	spec, err := BuildOpenAPICLISpec(body("image/png", `{"$ref":"#/components/schemas/Upload"}`), CliOptions{Binary: "x"}, "c")
	if err != nil {
		t.Fatalf("a $ref to a binary schema must generate: %v", err)
	}
	if op := spec.Ops[0]; op.BodyKind != "binary" || op.BodyContentType != "image/png" {
		t.Errorf("body = %q %q, want binary image/png", op.BodyKind, op.BodyContentType)
	}
	for _, ct := range []string{"multipart/form-data", "multipart/mixed", "application/x-www-form-urlencoded"} {
		for _, schema := range []string{`{"type":"string","format":"binary"}`, `{"$ref":"#/components/schemas/Upload"}`} {
			_, err := BuildOpenAPICLISpec(body(ct, schema), CliOptions{Binary: "x"}, "c")
			if err == nil || !strings.Contains(err.Error(), "request body offers "+ct) {
				t.Errorf("%s with %s: err = %v, want the unsupported-body refusal", ct, schema, err)
			}
		}
	}
}
