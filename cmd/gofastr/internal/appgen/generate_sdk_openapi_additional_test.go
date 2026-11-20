package appgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPISDKAdditionalPropertyNameCollision(t *testing.T) {
	for _, tc := range []struct{ wire, member string }{
		{"additionalProperties", "AdditionalProperties"},
		{"marshalJSON", "MarshalJSON"},
		{"unmarshalJSON", "UnmarshalJSON"},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			doc := oaSDKDoc(
				`{"/record":{"get":{"operationId":"getRecord","responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/RecordData"}}}}}}}}`,
				fmt.Sprintf(`{"schemas":{"RecordData":{"type":"object","properties":{%q:{"type":"string"}},"additionalProperties":true}}}`, tc.wire),
			)
			spec, err := oaSDKSpecFor(t, doc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = renderOASDKGoFiles(spec)
			want := fmt.Sprintf("property %q of RecordData derives Go member %s, reserved for additionalProperties", tc.wire, tc.member)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

func TestOpenAPISDKAdditionalPropertiesRoundTrip(t *testing.T) {
	out := oaSDKGenerate(t, `{
  "openapi": "3.1.0",
  "info": {"title":"Records", "version":"1"},
  "paths": {"/record": {
    "get": {"operationId":"getRecord", "responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/RecordData"}}}}}},
    "post": {"operationId":"postRecord", "requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/RecordData"}}}}, "responses":{"204":{"description":"saved"}}}
  }},
  "components": {"schemas":{"RecordData":{
    "type":"object",
    "properties":{
      "known":{"type":"string"},
      "node":{"$ref":"#/components/schemas/Node"},
      "flex":{
        "type":"object",
        "properties":{
          "label":{"type":"string"},
          "optional":{"type":"string"}
        },
        "additionalProperties":true
      },
      "emptySchema":{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":{}},
      "merged":{
        "allOf":[{"$ref":"#/components/schemas/Identified"}],
        "properties":{"label":{"type":"string"}},
        "additionalProperties":true
      },
      "mergedTyped":{"$ref":"#/components/schemas/TypedExtras"},
      "closed":{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":false},
      "implicit":{"type":"object","properties":{"label":{"type":"string"}}}
    },
    "additionalProperties":{"type":["integer","null"]}
  },"Identified":{"type":"object","properties":{"id":{"type":"string"}}},
  "TypedExtras":{
    "allOf":[
      {"$ref":"#/components/schemas/Identified"},
      {"type":"object","additionalProperties":{"type":["integer","null"]}}
    ]
  },"Node":{
    "type":"object",
    "properties":{
      "next":{"$ref":"#/components/schemas/Node"},
      "children":{"type":"array","items":{"$ref":"#/components/schemas/Node"}},
      "links":{"type":"object","additionalProperties":{"$ref":"#/components/schemas/Node"}},
      "plain":{"type":"object","properties":{"back":{"$ref":"#/components/schemas/Node"}}}
    },
    "additionalProperties":{"$ref":"#/components/schemas/Node"}
  }}}
}`, "--no-archive")
	goDir := filepath.Join(out, "go")
	if err := os.WriteFile(filepath.Join(goDir, "additional_test.go"), []byte(oaGoAdditionalProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-v", "-count=1", "./...")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated SDK: %v\n%s", err, output)
	} else {
		t.Logf("generated SDK:\n%s", output)
	}
}

const oaGoAdditionalProbe = `package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	client "example.com/barc-sdk"
)

func TestRoundTrip(t *testing.T) {
	const common = "{\"known\":\"x\",\"count\":9007199254740993,\"missing\":null," +
		"\"flex\":{\"label\":\"y\",\"extra\":{\"n\":9007199254740993}},\"emptySchema\":{\"other\":[true,null]}," +
		"\"merged\":{\"id\":\"1\",\"label\":\"x\",\"extra\":1},\"mergedTyped\":{\"id\":\"2\",\"count\":3,\"missing\":null},"
	const want = common + "\"closed\":{\"label\":\"z\"},\"implicit\":{\"label\":\"w\"}}"
	const response = common + "\"closed\":{\"label\":\"z\",\"ignored\":1},\"implicit\":{\"label\":\"w\",\"ignored\":2}}"
	post := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, response)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			post <- string(body)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	c := client.NewClient(srv.URL, nil)
	ctx := context.Background()
	record, err := c.GetRecord(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.PostRecord(ctx, *record); err != nil {
		t.Fatal(err)
	}
	var got string
	select {
	case got = <-post:
	default:
		t.Fatal("generated client did not POST a body")
	}
	if !reflect.DeepEqual(jsonValues(t, got), jsonValues(t, want)) {
		t.Fatalf("POST body = %s, want %s", got, want)
	}
	t.Logf("POST body = %s", got)
}

func TestAdditionalPropertyNames(t *testing.T) {
	const want = "{\"label\":\"y\",\"Label\":42}"
	var flex client.RecordDataFlex
	if err := json.Unmarshal([]byte(want), &flex); err != nil {
		t.Fatal(err)
	}
	flex.AdditionalProperties["label"] = json.RawMessage("0")
	flex.AdditionalProperties["optional"] = json.RawMessage("0")
	got, err := json.Marshal(flex)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jsonValues(t, string(got)), jsonValues(t, want)) {
		t.Fatalf("marshaled %s, want %s", got, want)
	}

	var record client.RecordData
	if err := json.Unmarshal([]byte("{\"count\":\"not an integer\"}"), &record); err == nil {
		t.Fatal("a string was accepted as a typed integer additional property")
	}
	var composed client.TypedExtras
	if err := json.Unmarshal([]byte("{\"count\":\"not an integer\"}"), &composed); err == nil {
		t.Fatal("a string was accepted as a typed integer additional property in allOf")
	}
}

func TestRecursiveMixedObject(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
	for _, tc := range []struct {
		name string
		link func(*client.Node)
	}{
		{"pointer", func(n *client.Node) { n.Next = n }},
		{"slice", func(n *client.Node) { n.Children = []client.Node{{}}; n.Children[0] = *n }},
		{"map", func(n *client.Node) { n.Links = map[string]client.Node{}; n.Links["loop"] = *n }},
		{"plain struct", func(n *client.Node) { n.Plain = &client.NodePlain{Back: n} }},
		{"additional map", func(n *client.Node) { n.AdditionalProperties = map[string]client.Node{}; n.AdditionalProperties["loop"] = *n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var node client.Node
			tc.link(&node)
			if _, err := json.Marshal(node); err == nil || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("json.Marshal error = %v, want cycle error", err)
			}
		})
	}

	leaf := &client.Node{}
	node := client.Node{
		Next: leaf,
		AdditionalProperties: map[string]client.Node{
			"left": {Next: leaf}, "right": {Next: leaf},
		},
	}
	node.AdditionalProperties["next"] = node
	const want = "{\"left\":{\"next\":{}},\"next\":{},\"right\":{\"next\":{}}}"
	got, err := json.Marshal(node)
	if err != nil || string(got) != want {
		t.Fatalf("json.Marshal = %s, %v; want %s", got, err, want)
	}
}

func jsonValues(t *testing.T, body string) any {
	t.Helper()
	var obj any
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		t.Fatal(err)
	}
	return obj
}
`
