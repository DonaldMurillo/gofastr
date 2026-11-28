package clitest

// CliGateSpecDoc is an OpenAPI document whose operations carry summary,
// with a PATCH under /odd spelling every name and string with quotes,
// backticks, backslashes and a comment closer.
func CliGateSpecDoc(summary string) map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"servers": []any{map[string]any{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-Api-Key"},
				"bearer": map[string]any{"type": "http", "scheme": "bearer"},
			},
		},
		"security": []any{map[string]any{"apiKey": []any{}}},
		"paths": map[string]any{
			"/odd/{id}/\"q`b\\*/é": map[string]any{
				"patch": map[string]any{
					"operationId": "oddThing",
					"summary":     "odd `names` */ \" \\ end",
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "description": "the */ id `x` \"q\" \\", "schema": map[string]any{"type": "integer"}},
						map[string]any{"name": "page.size", "in": "query", "explode": false, "schema": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
						map[string]any{"name": "Display Name", "in": "query", "schema": map[string]any{"type": "boolean"}},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/octet-stream; name=\"a\\\"b*/`\"": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
						},
					},
					"responses": map[string]any{"200": map[string]any{}},
				},
			},
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
				"put": map[string]any{
					"operationId": "uploadThing",
					"parameters":  []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
					"security":    []any{map[string]any{"bearer": []any{}}},
					"requestBody": map[string]any{
						"content": map[string]any{"application/octet-stream": map[string]any{}},
					},
					"responses": map[string]any{"204": map[string]any{}},
				},
			},
		},
	}
}
