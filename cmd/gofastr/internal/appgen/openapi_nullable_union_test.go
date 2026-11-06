package appgen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestNullableUnionFlagTypes(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		for schemaType, goType := range map[string]string{"string": "string", "integer": "int", "number": "float64", "boolean": "bool"} {
			t.Run(union+"/"+schemaType, func(t *testing.T) {
				schema := fmt.Sprintf(`{"%s":[{"type":"null"},{"type":%q}]}`, union, schemaType)
				document := mustDecodeDoc(t, fmt.Sprintf(`{"openapi":"3.1.0","paths":{"/items":{"post":{"operationId":"create","parameters":[{"name":"q","in":"query","schema":%s}],"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"value":%s}}}}}}}}}`, schema, schema))
				spec, err := BuildOpenAPICLISpec(document, CliOptions{Binary: "nullable"}, "client")
				if err != nil {
					t.Fatal(err)
				}
				operation := spec.Ops[0]
				if len(operation.QueryParams) != 1 || operation.QueryParams[0].GoType != goType || len(operation.BodyFields) != 1 || operation.BodyFields[0].GoType != goType {
					t.Fatalf("nullable union flags=%+v", operation)
				}
			})
		}
	}
}

func TestMultiTypeUnionParameterNamesUnion(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		t.Run(union, func(t *testing.T) {
			document := mustDecodeDoc(t, fmt.Sprintf(`{"openapi":"3.1.0","paths":{"/items":{"get":{"operationId":"list","parameters":[{"name":"q","in":"query","schema":{"%s":[{"type":"string"},{"type":"integer"},{"type":"null"}]}}]}}}}`, union))
			_, err := BuildOpenAPICLISpec(document, CliOptions{Binary: "union"}, "client")
			if err == nil || !strings.Contains(err.Error(), `parameter "q"`) || !strings.Contains(err.Error(), union) {
				t.Fatalf("multi-type parameter error=%v; want parameter name and %s", err, union)
			}
		})
	}
}

func TestNullableUnionReferenceTypes(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		t.Run(union, func(t *testing.T) {
			document := mustDecodeDoc(t, fmt.Sprintf(`{"openapi":"3.1.0","components":{"schemas":{"Value":{"type":"integer"},"Nil":{"type":"null"},"Choice":{"%s":[{"$ref":"#/components/schemas/Value"},{"$ref":"#/components/schemas/Nil"}]}}},"paths":{"/items":{"post":{"operationId":"create","parameters":[{"name":"q","in":"query","schema":{"$ref":"#/components/schemas/Choice"}}],"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"value":{"allOf":[{"$ref":"#/components/schemas/Choice"}]}}}}}}}}}}`, union))
			spec, err := BuildOpenAPICLISpec(document, CliOptions{Binary: "nullable"}, "client")
			if err != nil {
				t.Fatal(err)
			}
			operation := spec.Ops[0]
			if operation.QueryParams[0].GoType != "int" || len(operation.BodyFields) != 1 || operation.BodyFields[0].GoType != "int" {
				t.Fatalf("referenced nullable union flags=%+v", operation)
			}
		})
	}
}

func TestRecursiveUnionBodyUsesRawJSON(t *testing.T) {
	document := mustDecodeDoc(t, `{"openapi":"3.1.0","components":{"schemas":{"Choice":{"anyOf":[{"type":"null"},{"$ref":"#/components/schemas/Choice"}]}}},"paths":{"/items":{"post":{"operationId":"create","requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"value":{"$ref":"#/components/schemas/Choice"}}}}}}}}}}`)
	spec, err := BuildOpenAPICLISpec(document, CliOptions{Binary: "recursive"}, "client")
	if err != nil {
		t.Fatal(err)
	}
	if operation := spec.Ops[0]; operation.BodyKind != "json" || len(operation.BodyFields) != 0 {
		t.Fatalf("recursive union must retain raw JSON fallback: %+v", operation)
	}
}

func TestNullableUnionCLIFlags(t *testing.T) {
	for _, union := range []string{"oneOf", "anyOf"} {
		t.Run(union, func(t *testing.T) {
			schema := fmt.Sprintf(`{"%s":[{"type":"string"},{"type":"null"}]}`, union)
			binary := buildOAPets(t, oaTempModule(t), fmt.Sprintf(`{"openapi":"3.1.0","paths":{"/items":{"post":{"operationId":"create","parameters":[{"name":"q","in":"query","schema":%s}],"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"value":%s}}}}}}}}}`, schema, schema))
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests <- fmt.Sprintf("%s:%v", request.URL.Query().Get("q"), body["value"])
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{}`))
			}))
			defer server.Close()
			command := exec.Command(binary, "create", "--url", server.URL, "--q", "search", "--value", "body")
			command.Env = oaCLIEnv(t)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("nullable flags failed: %v\n%s", err, output)
			}
			select {
			case request := <-requests:
				if request != "search:body" {
					t.Fatalf("request=%q", request)
				}
			default:
				t.Fatal("nullable flags did not send a request")
			}
		})
	}
}
