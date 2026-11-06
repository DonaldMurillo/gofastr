package appgen

import (
	"strings"
	"testing"
)

// An allOf that wraps one scalar schema is that scalar: FastAPI writes an
// enum parameter with a default as {"allOf": [{"$ref": Color}], "default":
// "red"}, and the merge read every allOf as an object, so the parameter
// failed generation as non-scalar and the body property lost its flag.
func TestOpenAPIAllOfOfAScalar(t *testing.T) {
	doc := mustDecodeDoc(t, `{"openapi":"3.0.2",
	  "components":{"schemas":{
	    "Color":{"enum":["red","green"],"type":"string"},
	    "Count":{"type":"integer"},
	    "Item":{"type":"object","required":["name"],"properties":{
	      "name":{"type":"string"},
	      "color":{"allOf":[{"$ref":"#/components/schemas/Color"}],"default":"red"}}}}},
	  "paths":{"/items":{
	    "get":{"operationId":"readItems","parameters":[
	      {"name":"color","in":"query","schema":{"allOf":[{"$ref":"#/components/schemas/Color"}],"default":"red"}},
	      {"name":"n","in":"query","schema":{"allOf":[{"$ref":"#/components/schemas/Count"},{"description":"how many"}]}}]},
	    "post":{"operationId":"createItem","requestBody":{"required":true,
	      "content":{"application/json":{"schema":{"$ref":"#/components/schemas/Item"}}}}}}}}`)
	spec, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
	if err != nil {
		t.Fatalf("an allOf of one scalar schema must generate: %v", err)
	}
	ops := map[string]cliOp{}
	for _, op := range spec.Ops {
		ops[op.Command] = op
	}
	q := ops["read-items"].QueryParams
	if len(q) != 2 || q[0].GoType != "string" || q[1].GoType != "int" {
		t.Errorf("query params = %+v, want color string and n int", q)
	}
	var body []string
	for _, f := range ops["create-item"].BodyFields {
		body = append(body, f.Wire+":"+f.GoType)
	}
	if got := strings.Join(body, ","); got != "color:string,name:string" {
		t.Errorf("body fields = %s, want color:string,name:string", got)
	}

	// Members naming two different types are no one scalar: still refused.
	doc = mustDecodeDoc(t, `{"openapi":"3.0.2","paths":{"/a":{"get":{"operationId":"a","parameters":[
	  {"name":"v","in":"query","schema":{"allOf":[{"type":"string"},{"type":"integer"}]}}]}}}}`)
	if _, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c"); err == nil || !strings.Contains(err.Error(), "non-scalar") {
		t.Errorf("an allOf of string and integer must be refused as non-scalar, got %v", err)
	}
}

// A parameter is in path, query, header or cookie. Any other in (Swagger
// 2's body and formData, a typo like querry, none at all) was dropped
// without a word: the CLI generated, lacking the parameter, and a
// required one never reached the server.
func TestOpenAPIUnknownParamLocationFails(t *testing.T) {
	for _, in := range []string{"querry", "body", "formData", ""} {
		doc := mustDecodeDoc(t, `{"openapi":"3.0.3","paths":{"/a":{"get":{"operationId":"a","parameters":[
		  {"name":"limit","in":"`+in+`","required":true,"schema":{"type":"integer"}}]}}}}`)
		_, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
		if err == nil || !strings.Contains(err.Error(), `parameter "limit"`) || !strings.Contains(err.Error(), "path, query or header") {
			t.Errorf("in %q: err = %v, want a refusal naming the parameter", in, err)
		}
	}
}

// A $ref present but not a string was silently treated as "no $ref":
// the CLI generated from the ref-holding node as if it were the
// resolved schema. The document is malformed; say so.
func TestOpenAPINonStringRefFails(t *testing.T) {
	for _, ref := range []string{"5", "null", `["#/x"]`} {
		doc := mustDecodeDoc(t, `{"openapi":"3.0.3","paths":{"/a":{"get":{"operationId":"a","parameters":[
		  {"name":"v","in":"query","schema":{"$ref":`+ref+`}}]}}}}`)
		_, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
		if err == nil || !strings.Contains(err.Error(), "$ref: want a string") {
			t.Errorf("$ref %s: err = %v, want the wrong-typed $ref refusal", ref, err)
		}
	}
}

// A Swagger 2.0 document reads differently (host and basePath for the
// server, body and formData parameters, a type on the parameter itself),
// and generating from it as OpenAPI 3 dropped its base path, its body
// and every parameter's type. Refuse it, and any openapi other than 3.x.
func TestOpenAPINotVersion3Fails(t *testing.T) {
	for _, head := range []string{`"swagger":"2.0"`, `"openapi":"2.0"`, `"openapi":"4.0.0"`} {
		doc := mustDecodeDoc(t, `{`+head+`,"paths":{"/a":{"get":{"operationId":"a"}}}}`)
		_, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c")
		if err == nil || !strings.Contains(err.Error(), "OpenAPI 3") {
			t.Errorf("%s: err = %v, want the OpenAPI 3 refusal", head, err)
		}
	}
	for _, head := range []string{`"openapi":"3.0.3"`, `"openapi":"3.1.0"`} {
		doc := mustDecodeDoc(t, `{`+head+`,"paths":{"/a":{"get":{"operationId":"a"}}}}`)
		if _, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "x"}, "c"); err != nil {
			t.Errorf("%s: %v", head, err)
		}
	}
}
