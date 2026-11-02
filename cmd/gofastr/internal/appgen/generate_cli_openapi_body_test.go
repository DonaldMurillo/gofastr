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
