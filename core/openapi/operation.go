package openapi

// Operation represents a single OpenAPI operation (e.g. GET /users).
type Operation struct {
	Summary     string
	Description string
	OperationID string
	Tags        []string
	Parameters  []map[string]any
	RequestBody *map[string]any
	Responses   map[int]map[string]any
}

// NewOperation creates a blank Operation.
func NewOperation() *Operation {
	return &Operation{
		Responses: make(map[int]map[string]any),
	}
}

// AddParameter appends a parameter to the operation.
// location is one of "path", "query", "header", "cookie".
func (o *Operation) AddParameter(name, location, description string, required bool, schema map[string]any) {
	param := map[string]any{
		"name":        name,
		"in":          location,
		"description": description,
		"required":    required,
	}
	if schema != nil {
		param["schema"] = schema
	}
	o.Parameters = append(o.Parameters, param)
}

// SetRequestBody sets the request body for the operation.
func (o *Operation) SetRequestBody(contentType string, schema map[string]any, required bool) {
	content := map[string]any{
		"schema": schema,
	}
	o.RequestBody = &map[string]any{
		"required": required,
		"content": map[string]any{
			contentType: content,
		},
	}
}

// AddResponse registers a response for the given HTTP status code.
func (o *Operation) AddResponse(status int, description string, schema map[string]any) {
	resp := map[string]any{
		"description": description,
	}
	if schema != nil {
		resp["content"] = map[string]any{
			"application/json": map[string]any{
				"schema": schema,
			},
		}
	}
	o.Responses[status] = resp
}

// ToMap converts the Operation into a map suitable for inclusion in the
// OpenAPI paths object.
func (o *Operation) ToMap() map[string]any {
	m := map[string]any{}

	if o.Summary != "" {
		m["summary"] = o.Summary
	}
	if o.Description != "" {
		m["description"] = o.Description
	}
	if o.OperationID != "" {
		m["operationId"] = o.OperationID
	}
	if len(o.Tags) > 0 {
		m["tags"] = o.Tags
	}
	if len(o.Parameters) > 0 {
		m["parameters"] = o.Parameters
	}
	if o.RequestBody != nil {
		m["requestBody"] = *o.RequestBody
	}
	if len(o.Responses) > 0 {
		m["responses"] = o.Responses
	}

	return m
}
