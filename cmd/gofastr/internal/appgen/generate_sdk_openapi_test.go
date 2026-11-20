package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

// oaSDKFixture is a barcode API in gobarc's shape: JSON and binary
// answers, a binary upload, a bearer default and one apiKey operation,
// plus a Swashbuckle-style answer offering text/plain beside JSON, two
// springdoc-style */* answers (a schema and a binary string), and two
// operations whose optional parameters come before a required body.
const oaSDKFixture = `{
  "openapi": "3.1.0",
  "info": {"title": "barc", "version": "1.0.0"},
  "servers": [{"url": "https://barc.example.com/api"}],
  "security": [{"bearer": []}],
  "components": {
    "securitySchemes": {
      "bearer": {"type": "http", "scheme": "bearer"},
      "key": {"type": "apiKey", "in": "header", "name": "X-Barc-Key"}
    },
    "schemas": {
      "GenerateRequest": {
        "type": "object",
        "required": ["data", "symbology"],
        "properties": {
          "data": {"type": "string", "description": "the text to encode"},
          "symbology": {"type": "string", "enum": ["qr", "ean13"]},
          "scale": {"type": "integer"},
          "transparent": {"type": "boolean"},
          "size": {"$ref": "#/components/schemas/Size"}
        }
      },
      "Size": {"type": "object", "properties": {"width": {"type": "integer"}, "height": {"type": "integer"}}},
      "ValidateResult": {
        "type": "object",
        "required": ["valid"],
        "properties": {
          "valid": {"type": "boolean"},
          "errors": {"type": "array", "items": {"type": "string"}},
          "checksum": {"type": ["string", "null"]},
          "serial": {"type": "integer"},
          "meta": {"type": "object", "additionalProperties": {"type": "string"}},
          "extra": {}
        }
      }
    }
  },
  "paths": {
    "/generate": {
      "post": {
        "operationId": "generateBarcode",
        "summary": "Render one barcode",
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/GenerateRequest"}}}},
        "responses": {"200": {"description": "image", "content": {"image/png": {}, "image/svg+xml": {}}}}
      }
    },
    "/validate": {
      "post": {
        "operationId": "validateData",
        "requestBody": {"content": {"application/json": {"schema": {
          "type": "object", "required": ["data"],
          "properties": {"data": {"type": "string"}, "symbology": {"type": "string"}}
        }}}},
        "responses": {"200": {"description": "ok", "content": {
          "application/json": {"schema": {"$ref": "#/components/schemas/ValidateResult"}},
          "text/json": {"schema": {"$ref": "#/components/schemas/ValidateResult"}},
          "text/plain": {"schema": {"$ref": "#/components/schemas/ValidateResult"}}
        }}}
      }
    },
    "/opt": {
      "post": {
        "operationId": "postOpt",
        "parameters": [{"name": "dryRun", "in": "query", "schema": {"type": "boolean"}}],
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Size"}}}},
        "responses": {"204": {"description": "done"}}
      }
    },
    "/upload": {
      "put": {
        "operationId": "uploadOpt",
        "parameters": [{"name": "name", "in": "query", "schema": {"type": "string"}}],
        "requestBody": {"required": true, "content": {"application/octet-stream": {}}},
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "integer"}}}}}
      }
    },
    "/spec/{symbology}": {
      "get": {
        "operationId": "getSpec",
        "parameters": [
          {"name": "symbology", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "verbose", "in": "query", "schema": {"type": "boolean"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer"}},
          {"name": "tag", "in": "query", "schema": {"type": "array", "items": {"type": "string"}}},
          {"name": "fields", "in": "query", "style": "form", "explode": false, "schema": {"type": "array", "items": {"type": "string"}}},
          {"name": "X-Trace", "in": "header", "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
          "type": "object", "properties": {"name": {"type": "string"}, "lengths": {"type": "array", "items": {"type": "integer"}}}
        }}}}}
      }
    },
    "/decode": {
      "post": {
        "operationId": "decodeImage",
        "security": [{"key": []}],
        "requestBody": {"required": true, "content": {"image/png": {"schema": {"type": "string", "format": "binary"}}}},
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object", "properties": {"text": {"type": "string"}}}}}}}}
      }
    },
    "/batch": {
      "post": {
        "operationId": "batchZip",
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"type": "array", "items": {"$ref": "#/components/schemas/GenerateRequest"}}}}},
        "responses": {"200": {"description": "zip", "content": {"application/zip": {}}}}
      }
    },
    "/pet": {
      "get": {
        "operationId": "getPet",
        "responses": {"200": {"description": "springdoc", "content": {"*/*": {"schema": {"$ref": "#/components/schemas/Size"}}}}}
      }
    },
    "/pet/photo": {
      "get": {
        "operationId": "getPetPhoto",
        "responses": {"200": {"description": "springdoc", "content": {"*/*": {"schema": {"type": "string", "format": "binary"}}}}}
      }
    },
    "/ping": {
      "delete": {"operationId": "ping", "responses": {"204": {"description": "gone"}}}
    }
  }
}`

// oaSDKGenerate writes doc as openapi.json in a fresh module directory and
// runs `gofastr generate sdk --from-openapi` there.
func oaSDKGenerate(t *testing.T, doc string, args ...string) string {
	t.Helper()
	dir := oaTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	RunGenerateSDK(append([]string{"--from-openapi=openapi.json", "--name=barc", "--module=example.com/barc-sdk", "--sdk-version=1.2.3"}, args...))
	return filepath.Join(dir, "gen", "sdk")
}

func oaSDKSpecFor(t *testing.T, doc string) (oaSDKSpec, error) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return buildOpenAPISDKSpec(m, &SdkOptions{name: "barc", module: "example.com/barc-sdk", set: map[string]bool{}}, "openapi.json")
}

// The SDK writes the layout `generate sdk` writes for entities, and its
// manifest records the OpenAPI source and the document's hash in place
// of an entity list.
func TestOpenAPISDKLayoutAndManifest(t *testing.T) {
	out := oaSDKGenerate(t, oaSDKFixture)
	for _, f := range []string{"go/client.go", "go/operations.go", "go/go.mod", "go/README.md", "js/client.js", "js/client.d.ts", "js/README.md",
		"dist/" + sdk.GoArtifact, "dist/" + sdk.JSArtifact, "dist/" + sdk.JSTypesArtifact, "dist/" + sdk.ManifestFile} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	m, err := sdk.ReadManifest(os.DirFS(filepath.Join(out, "dist")))
	if err != nil {
		t.Fatal(err)
	}
	want, err := sdk.OpenAPIHash([]byte(oaSDKFixture))
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != sdk.SourceOpenAPI || len(m.Entities) != 0 || m.SchemaHash != want || m.App != "barc" || m.SDKVersion != "1.2.3" {
		t.Fatalf("manifest: %+v, want the openapi source and hash %s", m, want)
	}
	if m.TokenHeader != "Authorization" || m.TokenPrefix != "Bearer " {
		t.Errorf("manifest token scheme %q %q, want the document's bearer scheme", m.TokenHeader, m.TokenPrefix)
	}
	if m.Artifacts["go"].Module != "example.com/barc-sdk" || len(m.Artifacts) != 3 {
		t.Errorf("artifacts: %+v", m.Artifacts)
	}
}
