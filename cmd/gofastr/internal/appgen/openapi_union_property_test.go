package appgen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

func TestUnionPropertiesUseRawJSON(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		unionSchema := fmt.Sprintf(`{"%s":[{"type":"integer"},{"type":"boolean"}]}`, union)
		refSibling := fmt.Sprintf(`{"$ref":"#/components/schemas/Base","%s":[{"type":"integer"},{"type":"boolean"}]}`, union)
		for _, variant := range []struct{ name, property string }{
			{"direct", unionSchema},
			{"allOf-ref", `{"allOf":[{"$ref":"#/components/schemas/Value"}]}`},
			{"ref-sibling", refSibling},
			{"ref-chain", `{"$ref":"#/components/schemas/Alias"}`},
			{"allOf-ref-sibling", fmt.Sprintf(`{"allOf":[%s,{"type":"integer"}]}`, refSibling)},
			{"ref-allOf-sibling", fmt.Sprintf(`{"$ref":"#/components/schemas/Base","allOf":[%s,{"type":"integer"}]}`, unionSchema)},
			{"allOf-typed", fmt.Sprintf(`{"allOf":[{"type":"integer"},%s]}`, unionSchema)},
		} {
			t.Run(union+"/"+variant.name, func(t *testing.T) {
				doc := mustDecodeDoc(t, fmt.Sprintf(`{"openapi":"3.1.0","components":{"schemas":{"Base":{},"Alias":%s,"Value":%s}},
				"paths":{"/items":{"post":{"operationId":"create","requestBody":{"content":{"application/json":{"schema":{
				"type":"object","properties":{"count":%s,"label":{"type":"string"},"plain":{"$ref":"#/components/schemas/Base"}}}}}}}}}}`, refSibling, unionSchema, variant.property))
				spec, err := BuildOpenAPICLISpec(doc, CliOptions{Binary: "union"}, "client")
				if err != nil {
					t.Fatal(err)
				}
				if len(spec.Ops) != 1 || spec.Ops[0].BodyKind != "json" {
					t.Fatalf("raw JSON unavailable: %+v", spec.Ops)
				}
				fields := spec.Ops[0].BodyFields
				if len(fields) != 2 || fields[0].Wire != "label" || fields[1].Wire != "plain" || fields[1].GoType != "string" {
					t.Errorf("union emitted scalar flags: %+v", fields)
				}
			})
		}
	}
}

func TestRefUnionCLIUsesRawJSON(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, `{"openapi":"3.1.0","components":{"schemas":{"Base":{}}},
	"paths":{"/items":{"post":{"operationId":"create","requestBody":{"content":{"application/json":{"schema":{
	"type":"object","properties":{
	"count":{"$ref":"#/components/schemas/Base","oneOf":[{"type":"integer"},{"type":"boolean"}]},
	"choice":{"$ref":"#/components/schemas/Base","anyOf":[{"type":"integer"},{"type":"boolean"}]}
	}}}}}}}}}`)
	bodies := make(chan map[string]any, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	for _, field := range []string{"count", "choice"} {
		command := exec.Command(bin, "create", "--url", server.URL, "--"+field, "7")
		command.Env = oaCLIEnv(t)
		if output, err := command.CombinedOutput(); err == nil {
			t.Errorf("union flag --%s was accepted: %s", field, output)
		}
	}
	if len(bodies) != 0 {
		t.Fatalf("union flags sent %d requests", len(bodies))
	}
	command := exec.Command(bin, "create", "--url", server.URL, "--json", `{"count":7,"choice":true}`)
	command.Env = oaCLIEnv(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("raw JSON refused: %v\n%s", err, output)
	}
	select {
	case body := <-bodies:
		if body["count"] != float64(7) || body["choice"] != true {
			t.Errorf("raw JSON body=%v", body)
		}
	default:
		t.Fatal("raw JSON never reached the server")
	}
}
