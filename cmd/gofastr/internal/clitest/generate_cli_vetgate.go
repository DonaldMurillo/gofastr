package clitest

// CliGateSpecDoc builds one fixture OpenAPI document with the given
// operation summary: a GET with query and header parameters, a POST with
// a path parameter and a JSON body, apiKey security, and a server entry —
// enough surface to exercise the self-client, operations.go, and the
// shared scaffold both generation modes emit.
func CliGateSpecDoc(summary string) map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"servers": []any{map[string]any{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-Api-Key"},
			},
		},
		"security": []any{map[string]any{"apiKey": []any{}}},
		"paths": map[string]any{
			"/things": map[string]any{
				"get": map[string]any{
					"operationId": "listThings",
					"summary":     summary,
					"parameters": []any{
						map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer"}},
						map[string]any{"name": "X-Trace", "in": "header", "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{}}}},
				},
			},
			"/things/{id}": map[string]any{
				"post": map[string]any{
					"operationId": "createThing",
					"summary":     summary,
					"parameters":  []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
					"requestBody": map[string]any{
						"required": true,
						"content":  map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}}},
					},
					"responses": map[string]any{"201": map[string]any{}},
				},
			},
		},
	}
}
