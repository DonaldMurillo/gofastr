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
      "flex":{
        "type":"object",
        "properties":{
          "label":{"type":"string"},
          "optional":{"type":"string"}
        },
        "additionalProperties":true
      },
      "emptySchema":{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":{}},
      "closed":{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":false},
      "implicit":{"type":"object","properties":{"label":{"type":"string"}}}
    },
    "additionalProperties":{"type":["integer","null"]}
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
	"strings"
	"testing"

	client "example.com/barc-sdk"
)

func TestRoundTrip(t *testing.T) {
	const want = "{\"known\":\"x\",\"count\":9007199254740993,\"missing\":null,\"flex\":{\"label\":\"y\",\"extra\":{\"n\":9007199254740993}},\"emptySchema\":{\"other\":[true,null]},\"closed\":{\"label\":\"z\"},\"implicit\":{\"label\":\"w\"}}"
	const response = "{\"known\":\"x\",\"count\":9007199254740993,\"missing\":null,\"flex\":{\"label\":\"y\",\"extra\":{\"n\":9007199254740993}},\"emptySchema\":{\"other\":[true,null]},\"closed\":{\"label\":\"z\",\"ignored\":1},\"implicit\":{\"label\":\"w\",\"ignored\":2}}"
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
