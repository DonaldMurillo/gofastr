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
