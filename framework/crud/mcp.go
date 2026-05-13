package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// RegisterEntityMCPTools exposes a CRUD handler through MCP tools.
//
// router is the http.Handler that owns the entity's CRUD routes — typically
// app.Router. MCP tool calls are dispatched through router.ServeHTTP so they
// share the exact same middleware chain (auth, recovery, logging, security
// headers, etc.) as live HTTP traffic. Passing the bare CRUD handler would
// silently bypass that middleware.
func RegisterEntityMCPTools(server *mcp.Server, crud *CrudHandler, router http.Handler) error {
	if server == nil {
		return fmt.Errorf("entity mcp: server is nil")
	}
	if crud == nil || crud.Entity == nil {
		return fmt.Errorf("entity mcp: crud handler is nil")
	}
	if router == nil {
		return fmt.Errorf("entity mcp: router is nil — MCP CRUD tools must dispatch through the app router so middleware applies")
	}
	ent := crud.Entity.GetName()
	defs := []struct {
		name        string
		description string
		schema      map[string]any
		handler     mcp.ToolHandler
	}{
		{ent + "_list", "List " + ent + " records", listToolSchema(crud.Entity), crud.listTool(router)},
		{ent + "_get", "Get one " + ent + " record by id", idToolSchema(), crud.getTool(router)},
		{ent + "_create", "Create a " + ent + " record", writeToolSchema(crud.Entity), crud.createTool(router)},
		{ent + "_update", "Update a " + ent + " record", updateToolSchema(crud.Entity), crud.updateTool(router)},
		{ent + "_delete", "Delete a " + ent + " record by id", idToolSchema(), crud.deleteTool(router)},
	}
	for _, def := range defs {
		if err := server.RegisterTool(def.name, def.description, def.schema, def.handler); err != nil {
			return err
		}
	}
	return nil
}

func (ch *CrudHandler) listTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		values := make(url.Values)
		for _, key := range []string{"page", "limit", "sort"} {
			if v, ok := params[key]; ok {
				values.Set(key, fmt.Sprint(v))
			}
		}
		for _, field := range ch.Entity.GetFields() {
			for _, suffix := range []string{"", "_gt", "_gte", "_lt", "_lte", "_like", "_in"} {
				key := field.Name + suffix
				if v, ok := params[key]; ok {
					values.Set(key, fmt.Sprint(v))
				}
			}
		}
		path := "/" + ch.Entity.GetTable()
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return runToolRequest(ctx, router, http.MethodGet, path, nil)
	}
}

func (ch *CrudHandler) getTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		id, err := requireToolString(params, "id")
		if err != nil {
			return nil, err
		}
		return runToolRequest(ctx, router, http.MethodGet, "/"+ch.Entity.GetTable()+"/"+url.PathEscape(id), nil)
	}
}

func (ch *CrudHandler) createTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		return runToolRequest(ctx, router, http.MethodPost, "/"+ch.Entity.GetTable(), params)
	}
}

func (ch *CrudHandler) updateTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		id, err := requireToolString(params, "id")
		if err != nil {
			return nil, err
		}
		body := make(map[string]any, len(params)-1)
		for k, v := range params {
			if k != "id" {
				body[k] = v
			}
		}
		return runToolRequest(ctx, router, http.MethodPut, "/"+ch.Entity.GetTable()+"/"+url.PathEscape(id), body)
	}
}

func (ch *CrudHandler) deleteTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		id, err := requireToolString(params, "id")
		if err != nil {
			return nil, err
		}
		if _, err := runToolRequest(ctx, router, http.MethodDelete, "/"+ch.Entity.GetTable()+"/"+url.PathEscape(id), nil); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "id": id}, nil
	}
}

func runToolRequest(ctx context.Context, router http.Handler, method, path string, body any) (any, error) {
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader).WithContext(ctx)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	status := rec.Code
	if status >= 400 {
		return nil, fmt.Errorf("entity mcp request failed: status %d: %s", status, strings.TrimSpace(rec.Body.String()))
	}
	if status == http.StatusNoContent || rec.Body.Len() == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func requireToolString(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", fmt.Errorf("missing required param %q", key)
	}
	s := fmt.Sprint(value)
	if s == "" {
		return "", fmt.Errorf("param %q must not be empty", key)
	}
	return s, nil
}

func idToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	}
}

func listToolSchema(ent *entity.Entity) map[string]any {
	props := map[string]any{
		"page":  map[string]any{"type": "integer", "minimum": 1},
		"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"sort":  map[string]any{"type": "string"},
	}
	for _, field := range ent.GetFields() {
		if field.Hidden {
			continue
		}
		props[field.Name] = mcpFieldSchema(field)
	}
	return map[string]any{"type": "object", "properties": props}
}

func writeToolSchema(ent *entity.Entity) map[string]any {
	props := make(map[string]any)
	var required []string
	for _, field := range ent.GetFields() {
		if field.AutoGenerate != schema.AutoNone || field.ReadOnly || field.Hidden {
			continue
		}
		props[field.Name] = mcpFieldSchema(field)
		if field.Required && field.Default == nil {
			required = append(required, field.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func updateToolSchema(ent *entity.Entity) map[string]any {
	out := writeToolSchema(ent)
	props := out["properties"].(map[string]any)
	props["id"] = map[string]any{"type": "string"}
	out["required"] = []string{"id"}
	return out
}

func mcpFieldSchema(field schema.Field) map[string]any {
	switch field.Type {
	case schema.Int:
		return map[string]any{"type": "integer"}
	case schema.Float, schema.Decimal:
		return map[string]any{"type": "number"}
	case schema.Bool:
		return map[string]any{"type": "boolean"}
	case schema.JSON:
		return map[string]any{"type": "object"}
	case schema.Enum:
		return map[string]any{"type": "string", "enum": field.Values}
	default:
		return map[string]any{"type": "string"}
	}
}
