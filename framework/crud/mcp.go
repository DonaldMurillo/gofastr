package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// RegisterEntityMCPTools exposes a CRUD handler through MCP tools.
//
// router is the http.Handler that owns the entity's CRUD routes, typically
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
		return fmt.Errorf("entity mcp: router is nil: MCP CRUD tools must dispatch through the app router so middleware applies")
	}
	ent := crud.Entity.GetName()
	// Tool name shape:
	//   no namespace      → "<entity>_<action>"   (historical flat names, public agent surface)
	//   namespace set     → "<ns>.<entity>.<action>" (per routegroup.WithMCPNamespace docs)
	// The flat form MUST stay stable for unversioned/no-namespace entities;
	// the namespaced dot form disambiguates two versions of the same entity.
	toolName := func(action string) string {
		if crud.MCPNamespace == "" {
			return ent + "_" + action
		}
		return crud.MCPNamespace + "." + ent + "." + action
	}
	// devImplied: the entity did not opt into MCP exposure itself
	// (Exposure.MCP unset), so this registration exists only because the
	// dev loop implied it (the app registers tools under
	// Exposure.MCP || (CRUD && dev.DevMCPEnabled())). The WRITE tools of
	// a dev-implied entity register UNGATED, which is only safe on a
	// loopback listener — mark them WithDevImplied so the framework's
	// bind guard withdraws exactly those on an exposed bind. An explicit
	// Exposure.MCP is a production choice and is never marked or
	// withdrawn. list/get stay: they are reads behind the same
	// row-scoping the HTTP routes apply.
	devImplied := crud.Entity.Config.Exposure == nil || !crud.Entity.Config.Exposure.MCP
	defs := []struct {
		name        string
		description string
		schema      map[string]any
		handler     mcp.ToolHandler
		op          crudOp
		item        bool // the route judges one record: Ref{Type, ID}
		write       bool // dev-implied entities mark their write tools
	}{
		{toolName("list"), "List " + ent + " records", listToolSchema(crud.Entity), crud.listTool(router), opRead, false, false},
		{toolName("get"), "Get one " + ent + " record by id", idToolSchema(), crud.getTool(router), opRead, true, false},
		{toolName("create"), "Create a " + ent + " record", writeToolSchema(crud.Entity), crud.createTool(router), opCreate, false, true},
		{toolName("update"), "Update a " + ent + " record", updateToolSchema(crud.Entity), crud.updateTool(router), opUpdate, true, true},
		{toolName("delete"), "Delete a " + ent + " record by id", idToolSchema(), crud.deleteTool(router), opDelete, true, true},
	}
	for _, def := range defs {
		// Every tool carries the Access permission its route enforces
		// for this operation. The call was already refused through the
		// router; the gate also drops the tool from tools/list, so a
		// caller without posts:delete does not see posts_delete or its
		// input schema.
		opts := []mcp.ToolOption{mcp.WithToolGate(crud.mcpToolGate(def.op, def.item))}
		if devImplied && def.write {
			opts = append(opts, mcp.WithDevImplied())
		}
		if err := server.RegisterTool(def.name, def.description, def.schema, def.handler, opts...); err != nil {
			return err
		}
	}
	return nil
}

// errMCPToolForbidden is the gate's refusal. It names no permission: the
// listing already hides the tool, and a caller probing by name learns only
// that it may not call it.
var errMCPToolForbidden = fmt.Errorf("entity mcp: not permitted")

// mcpToolGate returns the per-caller precondition for one entity tool:
// the entity's Access permission for op, the check requirePermission runs
// on the route, at the same Ref{Type} the route uses for list and create.
// get, update and delete (item) are judged on the route at Ref{Type, ID},
// and a Decider may answer per record, so with a Decider on the context
// an item tool is left to the route; a role policy is record-blind and
// still judges it.
//
// It judges only what the MCP request's own context can show. With no
// user on it, the credentials may still be resolved on the redispatch (the
// API key and cookies are copied onto the in-process request and the
// router's auth middleware runs there). With no role policy and no
// Decider on it, the policy may be mounted on a route group, which only
// the redispatch passes through. Either way the gate cannot know the
// answer, so it leaves the tool listed and the route refuses the call as
// it always did. Owner and tenant scoping are left to the route too: they
// narrow rows rather than refuse the entity, and their context may only
// exist after the router's middleware. An entity mounted on a route group
// (MCPRouteScoped) is left to the route entirely: the group's own
// middleware may replace the policy the /mcp context carries.
func (ch *CrudHandler) mcpToolGate(op crudOp, item bool) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if ch.MCPRouteScoped {
			return nil
		}
		if _, ok := handler.GetUser(ctx); !ok {
			return nil
		}
		decider := access.GetDecider(ctx)
		if access.PolicyFromContext(ctx) == nil && decider == nil {
			return nil
		}
		if item && decider != nil {
			return nil
		}
		perm := ch.permissionForOp(op)
		if perm == "" {
			return nil
		}
		if access.CanResource(ctx, access.Permission(perm), access.Ref{Type: ch.Entity.GetName()}) {
			return nil
		}
		return errMCPToolForbidden
	}
}

// mcpBase is the URL path the entity's HTTP routes are mounted at. BasePath
// (e.g. "/api/v1", set by the app from WithAPIPrefix) is prepended so the
// in-process MCP tool dispatch reaches the same path the REST routes live at;
// empty BasePath yields the historical bare "/table".
func (ch *CrudHandler) mcpBase() string {
	return ch.BasePath + "/" + ch.Entity.GetTable()
}

func (ch *CrudHandler) listTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		values := make(url.Values)
		for _, key := range []string{"page", "limit", "sort"} {
			if v, ok := params[key]; ok {
				values[key] = toolParamValues(v)
			}
		}
		for _, field := range ch.snapshotFields() {
			// Never build a filter predicate on a Hidden field. Its value
			// is omitted from output, but a filter turns row presence /
			// absence into a value-disclosure oracle over a secret column.
			// Mirrors listToolSchema, which also skips Hidden fields.
			// NoQuery fields are skipped for the same reason: the value is
			// returned in a transformed form, so filtering on the stored
			// one rebuilds the oracle.
			//
			// Skipping means the key is never forwarded, so the request
			// runs UNFILTERED rather than returning the 400 the HTTP
			// surface would give. That is deliberate and pinned by
			// TestMCP_ListToolOmitsHiddenFieldFilters. Neither field kind is
			// advertised in listToolSchema, so a well-formed tool call never
			// sends one; a malformed one gets a wider result set, not a
			// narrower one, and never an oracle bit.
			if field.Hidden || field.NoQuery {
				continue
			}
			// Plain equality first (the suffix table carries no ""
			// entry, and every field accepts equals), then one key per
			// operator from filter.FilterSuffixes ∩ OpSuitsType — the
			// same predicate the HTTP list handler applies — so the tool
			// forwards exactly the params listToolSchema advertises and
			// the route accepts. An operator the field's type refuses
			// (a _gt on a Bool) is dropped, not forwarded: the same
			// wider-not-narrower posture as the Hidden/NoQuery skip
			// above, and it was never advertised, so a well-formed call
			// never sends one.
			if v, ok := params[mcpFieldKey(field)]; ok {
				values[mcpFieldKey(field)] = toolParamValues(v)
			}
			for _, s := range filter.FilterSuffixes {
				if !filter.OpSuitsType(s.Op, field.Type) {
					continue
				}
				key := mcpFieldKey(field) + s.Suffix
				if v, ok := params[key]; ok {
					values[key] = toolParamValues(v)
				}
			}
		}
		// ?q= free-text search: forwarded to the list URL when the entity
		// declares SearchFields (the schema below advertises it only then).
		if len(ch.Entity.Config.SearchFields) > 0 {
			if v, ok := params["q"]; ok {
				values["q"] = toolParamValues(v)
			}
		}
		path := ch.mcpBase()
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return runToolRequest(ctx, router, http.MethodGet, path, nil)
	}
}

// toolParamValues renders one tool-call parameter for the re-dispatch query.
// A JSON array (the natural spelling an MCP client sends for a multi-value
// _in filter) becomes one query entry per element: fmt.Sprint stringifies
// []any{"draft","archived"} into "[draft archived]", a single literal that
// matches nothing, so the agent gets a silent empty page where the HTTP
// surface returns the union.
//
// Numbers render as their exact decimal: an integral float64 prints via
// FormatFloat('f', -1) — fmt.Sprint's %g gives "1e+06" for 1000000, which
// the filter surface cannot address on Postgres — and a json.Number (a
// transport that decoded with UseNumber) prints verbatim so a literal
// above 2^53 keeps every digit on its way into the query string.
func toolParamValues(v any) []string {
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			out = append(out, toolParamString(e))
		}
		return out
	}
	return []string{toolParamString(v)}
}

// toolParamString renders one scalar tool parameter for a query-string
// filter value.
func toolParamString(v any) string {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) && math.Abs(x) < 1e21 {
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
	}
	return fmt.Sprint(v)
}

func (ch *CrudHandler) getTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		id, err := requireToolString(params, "id")
		if err != nil {
			return nil, err
		}
		return unwrapToolData(runToolRequest(ctx, router, http.MethodGet, ch.mcpBase()+"/"+url.PathEscape(id), nil))
	}
}

func (ch *CrudHandler) createTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		return unwrapToolData(runToolRequest(ctx, router, http.MethodPost, ch.mcpBase(), params))
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
		return unwrapToolData(runToolRequest(ctx, router, http.MethodPatch, ch.mcpBase()+"/"+url.PathEscape(id), body))
	}
}

func unwrapToolData(result any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	envelope, ok := result.(map[string]any)
	if !ok {
		return result, nil
	}
	data, ok := envelope["data"]
	if !ok {
		return result, nil
	}
	return data, nil
}

func (ch *CrudHandler) deleteTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		id, err := requireToolString(params, "id")
		if err != nil {
			return nil, err
		}
		if _, err := runToolRequest(ctx, router, http.MethodDelete, ch.mcpBase()+"/"+url.PathEscape(id), nil); err != nil {
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
	// The internal request is re-dispatched through the full middleware
	// chain (auth, recovery, etc.). Session/JWT middleware re-resolves the
	// caller from transport headers, NOT from ctx, so without copying the
	// original request's auth the caller is demoted to anonymous and
	// owner-scoped CRUD returns 401. Copy every credential header the
	// framework recognizes from the original inbound request (stashed by
	// the MCP transport) so the same identity re-resolves. See
	// TestMCPAuthenticatedListReturnsOwnerRows.
	//
	// Every FIELD, never Get()+Set(): Get returns only the FIRST field, and
	// Cookie routinely arrives as several (a proxy prepends its own before
	// the browser's), so Set(Get()) collapses them to the shared first
	// field and loses the session — the same collapse Broker.MintDelegation
	// had (issue #360).
	//
	// X-API-Key belongs in the list because battery/auth's apitoken resolves
	// it like a session; without it an API-key caller was anonymous on every
	// re-dispatch (issue #360's second layer). The embed grant stays because
	// embed.Host.Middleware short-circuits on a missing header — dropping it
	// would authenticate the subject without ever evaluating the reach
	// allow-list or the grant's expiry for the path being re-dispatched to.
	// Nothing beyond these four rides along: the re-dispatch re-presents
	// the caller's credentials, never other headers the original carried.
	if orig, ok := mcp.RequestFromContext(ctx); ok && orig != nil {
		for _, name := range []string{"Cookie", "Authorization", "X-API-Key", fembed.GrantHeader} {
			for _, v := range orig.Header.Values(name) {
				if v == "" {
					continue
				}
				req.Header.Add(name, v)
			}
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	status := rec.Code
	if status >= 400 {
		// An *RPCError, not a plain error: the re-dispatched handler's
		// refusals (401 anonymous, 403 scope, 404 miss) are
		// caller-actionable and must stay visible through the MCP
		// boundary; a plain error is internal detail the server redacts
		// to "internal tool error", which would tell an agent the tool
		// is broken when the truth is the CALL lacks authority.
		return nil, &mcp.RPCError{
			Code:    mcp.ErrInternalError,
			Message: fmt.Sprintf("entity mcp request failed: status %d: %s", status, strings.TrimSpace(rec.Body.String())),
		}
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
	if len(ent.Config.SearchFields) > 0 {
		props["q"] = map[string]any{"type": "string", "description": "Free-text search across: " + strings.Join(ent.Config.SearchFields, ", ")}
	}
	for _, field := range ent.GetFields() {
		// These props are the list tool's FILTER arguments, so NoQuery
		// fields are omitted alongside Hidden ones, an agent offered a
		// filter the parser rejects just burns a call on a 400.
		if field.Hidden || field.NoQuery {
			continue
		}
		props[mcpFieldKey(field)] = mcpFieldSchema(field)
		// One prop per operator the field's type accepts, from the same
		// filter.FilterSuffixes ∩ OpSuitsType derivation the forwarding
		// loop uses: the schema advertises exactly the field/operator
		// params the HTTP list handler accepts — an operator the parser
		// refuses is never offered, one it accepts never hidden.
		for _, s := range filter.FilterSuffixes {
			if !filter.OpSuitsType(s.Op, field.Type) {
				continue
			}
			ps := mcpFieldSchema(field)
			if s.Op == filter.OpLike {
				// A substring, never a whole value: an Enum's value list
				// would make a client refuse "pai" for "paid".
				ps = map[string]any{"type": "string"}
			}
			if s.Op == filter.OpIn {
				// A JSON array is the natural spelling an MCP client
				// sends, and toolParamValues expands it into the
				// repeated query entries the route parses.
				ps = map[string]any{"type": "array", "items": mcpFieldSchema(field)}
			}
			ps["description"] = fmt.Sprintf("Filter by %s: %s.", field.Name, mcpOperatorLabel(s.Op))
			props[mcpFieldKey(field)+s.Suffix] = ps
		}
	}
	return map[string]any{"type": "object", "properties": props}
}

// mcpOperatorLabel names one filter operator in a tool-schema
// description.
func mcpOperatorLabel(op filter.FilterOp) string {
	switch op {
	case filter.OpEq:
		return "equals"
	case filter.OpNe:
		return "not equal"
	case filter.OpGt:
		return "greater than"
	case filter.OpGte:
		return "greater than or equal"
	case filter.OpLt:
		return "less than"
	case filter.OpLte:
		return "less than or equal"
	case filter.OpLike:
		return "contains the substring"
	case filter.OpIn:
		return "is one of"
	}
	return "matches"
}

func writeToolSchema(ent *entity.Entity) map[string]any {
	props := make(map[string]any)
	var required []string
	for _, field := range ent.GetFields() {
		if field.AutoGenerate != schema.AutoNone || field.ReadOnly || field.Hidden {
			continue
		}
		key := mcpFieldKey(field)
		props[key] = mcpFieldSchema(field)
		if field.Required && field.Default == nil {
			required = append(required, key)
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

// mcpFieldKey returns the parameter name a field is exposed under in MCP
// tool schemas: WireName when set (the version-specific alias), else the
// raw DB column Name. The CRUD filter parser accepts both forms.
func mcpFieldKey(field schema.Field) string {
	if field.WireName != "" {
		return field.WireName
	}
	return field.Name
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
