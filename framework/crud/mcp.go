package crud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// RegisterEntityMCPTools registers crud's list, get, create, update and
// delete tools, and one per routable move, on server. Each call is served
// through router, which must own the entity's routes, so the app's
// middleware runs on it.
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
	toolName := func(action string) string {
		if crud.MCPNamespace == "" {
			return ent + "_" + action
		}
		return crud.MCPNamespace + "." + ent + "." + action
	}
	devImplied := crud.Entity.Config.Exposure == nil || !crud.Entity.Config.Exposure.MCP
	type toolDef struct {
		name        string
		description string
		schema      map[string]any
		handler     mcp.ToolHandler
		op          crudOp
		item        bool
		write       bool
		permission  access.Permission
		hints       mcp.Annotations
	}
	keyArgs := crud.toolKeyArgs()
	reg := crud.Registry
	defs := []toolDef{
		{toolName("list"), "List " + ent + " records", listToolSchema(crud.Entity, reg), crud.listTool(router), opRead, false, false, "", readHints},
		{toolName("get"), "Get one " + ent + " record by id", idToolSchema(crud.Entity, reg, keyArgs), crud.getTool(router), opRead, true, false, "", readHints},
		{toolName("create"), "Create a " + ent + " record", writeToolSchema(crud.Entity, reg), crud.createTool(router), opCreate, false, true, "", createHints},
		{toolName("update"), "Update a " + ent + " record", updateToolSchema(crud.Entity, reg, keyArgs), crud.updateTool(router), opUpdate, true, true, "", updateHints},
		{toolName("delete"), "Delete a " + ent + " record by id", idToolSchema(crud.Entity, reg, keyArgs), crud.deleteTool(router), opDelete, true, true, "", deleteHints},
	}
	for _, t := range RoutableTransitions(crud.Entity.Config.States) {
		defs = append(defs, toolDef{toolName(t.Key), transitionToolDescription(ent, crud.Entity.Config.States.Field, t), idToolSchema(crud.Entity, reg, keyArgs), crud.transitionTool(router, t.Key), opUpdate, true, true, access.Permission(t.Permission), transitionHints})
	}
	for _, def := range defs {
		opts := []mcp.ToolOption{
			mcp.WithToolGate(crud.mcpToolGate(def.op, def.item, def.permission)),
			mcp.WithAnnotations(def.hints),
		}
		if devImplied && def.write {
			opts = append(opts, mcp.WithDevImplied())
		}
		if err := server.RegisterTool(def.name, def.description, def.schema, def.handler, opts...); err != nil {
			return err
		}
	}
	return nil
}

var (
	readHints       = crudHints(true, false, true)
	createHints     = crudHints(false, false, false)
	updateHints     = crudHints(false, true, true)
	deleteHints     = crudHints(false, true, true)
	transitionHints = crudHints(false, true, false)
)

func crudHints(readOnly, destructive, idempotent bool) mcp.Annotations {
	return mcp.Annotations{
		ReadOnlyHint:    mcp.Bool(readOnly),
		DestructiveHint: mcp.Bool(destructive),
		IdempotentHint:  mcp.Bool(idempotent),
		OpenWorldHint:   mcp.Bool(false),
	}
}

var errMCPToolForbidden = fmt.Errorf("entity mcp: not permitted")

// mcpToolGate refuses a tool to a caller the route would refuse for op,
// where the MCP request's own context can tell; otherwise it lists the tool
// and leaves the call to the route.
func (ch *CrudHandler) mcpToolGate(op crudOp, item bool, permission access.Permission) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if ch.MCPRouteScoped {
			return nil
		}
		if ch.permissionForOp(op) == permRefused && !ch.accessAllows(ctx, permRefused, "") {
			return errMCPToolForbidden
		}
		if _, ok := handler.GetUser(ctx); !ok && carriesRedispatchCredential(ctx) {
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
		if perm != "" && !ch.accessAllows(ctx, perm, "") {
			return errMCPToolForbidden
		}
		if permission != "" && !access.CanResourceExact(ctx, permission, access.Ref{Type: ch.Entity.GetName()}) {
			return errMCPToolForbidden
		}
		return nil
	}
}

// mcpBase is the path the entity's HTTP routes are mounted at, BasePath included.
func (ch *CrudHandler) mcpBase() string {
	return ch.BasePath + "/" + ch.Entity.GetTable()
}

func (ch *CrudHandler) listTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		values := make(url.Values)
		for _, key := range []string{"page", "limit", "per_page", "offset", "sort"} {
			if v, ok := params[key]; ok {
				values[key] = toolParamValues(v)
			}
		}
		fields := ch.snapshotFields()
		spell := filter.SpellerFor(fields)
		for _, field := range fields {
			if field.Hidden || field.NoQuery {
				continue
			}
			eqKey := spell.Key(mcpFieldKey(field), filter.OpEq)
			if v, ok := params[eqKey]; ok {
				if !filter.OpSuitsType(filter.OpEq, field.Type) {
					return nil, toolArgError("param %q: a json field takes no filter", eqKey)
				}
				vs, err := toolFilterValues(eqKey, false, v)
				if err != nil {
					return nil, err
				}
				values[eqKey] = vs
			}
			for _, s := range filter.FilterSuffixes {
				if !filter.OpSuitsType(s.Op, field.Type) {
					continue
				}
				key := spell.Key(mcpFieldKey(field), s.Op)
				if v, ok := params[key]; ok {
					vs, err := toolFilterValues(key, s.Op == filter.OpIn, v)
					if err != nil {
						return nil, err
					}
					values[key] = vs
				}
			}
		}
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

// toolParamValues renders a tool parameter as query values, one per
// element of a JSON array.
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

// toolFilterValues renders one list-tool filter parameter as query values:
// an _in key takes a non-empty array of comma-free values, any other key one scalar.
func toolFilterValues(key string, in bool, v any) ([]string, error) {
	one := func(e any) (string, error) {
		switch e.(type) {
		case []any, map[string]any:
			return "", toolArgError("param %q takes one value, not a list or an object", key)
		}
		return toolParamString(e), nil
	}
	arr, isArr := v.([]any)
	if !in || !isArr {
		s, err := one(v)
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	}
	if len(arr) == 0 {
		return nil, toolArgError("param %q: an empty list matches no rows; send at least one value", key)
	}
	out := make([]string, 0, len(arr))
	for i, e := range arr {
		s, err := one(e)
		if err != nil {
			return nil, err
		}
		if strings.Contains(s, ",") {
			return nil, toolArgError("param %q: element %d holds a comma, which the list route reads as a separator", key, i)
		}
		out = append(out, s)
	}
	return out, nil
}

// toolParamString renders a scalar, an integral number as its exact decimal.
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
		path, _, err := ch.toolRecordPath(params)
		if err != nil {
			return nil, err
		}
		return unwrapToolData(runToolRequest(ctx, router, http.MethodGet, path, nil))
	}
}

func (ch *CrudHandler) createTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		return unwrapToolData(runToolRequest(ctx, router, http.MethodPost, ch.mcpBase(), params))
	}
}

func (ch *CrudHandler) updateTool(router http.Handler) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		path, args, err := ch.toolRecordPath(params)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any, len(params))
		for k, v := range params {
			if _, isKey := args[k]; !isKey {
				body[k] = v
			}
		}
		return unwrapToolData(runToolRequest(ctx, router, http.MethodPatch, path, body))
	}
}

func (ch *CrudHandler) transitionTool(router http.Handler, key string) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		path, _, err := ch.toolRecordPath(params)
		if err != nil {
			return nil, err
		}
		path += "/transitions/" + url.PathEscape(key)
		return unwrapToolData(runToolRequest(ctx, router, http.MethodPost, path, map[string]any{}))
	}
}

func transitionToolDescription(ent, field string, t entity.Transition) string {
	label := t.Label
	if label == "" {
		label = t.Key
	}
	d := fmt.Sprintf("%s: move a %s record's %s from %s to %s", label, ent, field, strings.Join(t.From, " or "), t.To)
	if t.Stamp != "" {
		d += ", setting " + t.Stamp + " to today"
	}
	return d
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
		path, args, err := ch.toolRecordPath(params)
		if err != nil {
			return nil, err
		}
		if _, err := runToolRequest(ctx, router, http.MethodDelete, path, nil); err != nil {
			return nil, err
		}
		out := map[string]any{"deleted": true}
		for k, v := range args {
			out[k] = v
		}
		return out, nil
	}
}

var redispatchCredentialHeaders = []string{"Cookie", "Authorization", "X-API-Key", fembed.GrantHeader}

func carriesRedispatchCredential(ctx context.Context) bool {
	orig, ok := mcp.RequestFromContext(ctx)
	if !ok || orig == nil {
		return false
	}
	for _, name := range redispatchCredentialHeaders {
		for _, v := range orig.Header.Values(name) {
			if v != "" {
				return true
			}
		}
	}
	return false
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
	if orig, ok := mcp.RequestFromContext(ctx); ok && orig != nil {
		req.RemoteAddr = orig.RemoteAddr
		req.Host = orig.Host
		for _, name := range append(slices.Clone(redispatchCredentialHeaders), []string{"X-Forwarded-For", "X-Real-IP"}...) {
			for _, v := range orig.Header.Values(name) {
				if v == "" {
					continue
				}
				req.Header.Add(name, v)
			}
		}
		if name, token, ok := middleware.VerifiedCSRFSubmission(orig.Context()); ok {
			req.Header.Set(name, token)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	status := rec.Code
	if status >= 400 {
		return nil, &mcp.RPCError{
			Code:    mcp.ErrInternalError,
			Message: fmt.Sprintf("entity mcp request failed: status %d: %s", status, strings.TrimSpace(rec.Body.String())),
		}
	}
	if status == http.StatusNoContent || rec.Body.Len() == 0 {
		return nil, nil
	}
	var out any
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func toolArgError(format string, args ...any) error {
	return &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: fmt.Sprintf(format, args...)}
}

func requireToolString(params map[string]any, key string) (string, error) {
	value, ok := params[key]
	if !ok {
		return "", toolArgError("missing required param %q", key)
	}
	s := toolParamString(value)
	if s == "" {
		return "", toolArgError("param %q must not be empty", key)
	}
	return s, nil
}

// toolKeyArgs names a record's key arguments: "id", or each composite key column's mcpFieldKey.
func (ch *CrudHandler) toolKeyArgs() []string {
	cols := ch.keyCols()
	if len(cols) <= 1 {
		return []string{"id"}
	}
	args := slices.Clone(cols)
	for i, c := range cols {
		for _, f := range ch.Entity.GetFields() {
			if f.Name == c {
				args[i] = mcpFieldKey(f)
			}
		}
	}
	return args
}

func (ch *CrudHandler) toolRecordPath(params map[string]any) (string, map[string]string, error) {
	keys := ch.toolKeyArgs()
	args := make(map[string]string, len(keys))
	path := ch.mcpBase()
	for _, k := range keys {
		v, err := requireToolString(params, k)
		if err != nil {
			return "", nil, err
		}
		args[k] = v
		path += "/" + url.PathEscape(v)
	}
	return path, args, nil
}

func idToolSchema(ent *entity.Entity, reg entity.Registry, args []string) map[string]any {
	tf := toolFieldsOf(reg, ent)
	props := make(map[string]any, len(args))
	for _, a := range args {
		props[a] = keyArgSchema(ent, tf, a)
	}
	return map[string]any{
		"type":       "object",
		"required":   slices.Clone(args),
		"properties": props,
	}
}

// keyArgSchema types a key argument by its key column's field; "id" names
// the single key column whatever it is called.
func keyArgSchema(ent *entity.Entity, tf toolFields, arg string) map[string]any {
	if arg == "id" && len(ent.KeyColumns()) == 1 {
		col := ent.KeyColumns()[0]
		for _, f := range tf.fields {
			if f.Name == col || mcpFieldKey(f) == col {
				return tf.schema(f)
			}
		}
		return map[string]any{"type": "string"}
	}
	for _, f := range tf.fields {
		if f.Name == arg || mcpFieldKey(f) == arg {
			return tf.schema(f)
		}
	}
	return map[string]any{"type": "string"}
}

func listToolSchema(ent *entity.Entity, reg entity.Registry) map[string]any {
	_, maximum := ListLimits(ent.Config.Pagination.MaxListLimit)
	props := map[string]any{
		"page":     map[string]any{"type": "integer", "minimum": 1},
		"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": maximum},
		"per_page": map[string]any{"type": "integer", "minimum": 1, "maximum": maximum, "description": "Alias for limit; limit wins when both are sent."},
		"offset":   map[string]any{"type": "integer", "minimum": 0, "description": "Rows to skip, replacing the page-derived skip (offset mode only)."},
		"sort":     map[string]any{"type": "string"},
	}
	if len(ent.Config.SearchFields) > 0 {
		props["q"] = map[string]any{"type": "string", "description": "Free-text search across: " + strings.Join(ent.Config.SearchFields, ", ")}
	}
	spell := filter.SpellerFor(ent.GetFields())
	tf := toolFieldsOf(reg, ent)
	for _, field := range tf.fields {
		if field.Hidden || field.NoQuery {
			continue
		}
		if filter.OpSuitsType(filter.OpEq, field.Type) {
			props[spell.Key(mcpFieldKey(field), filter.OpEq)] = tf.schema(field)
		}
		for _, s := range filter.FilterSuffixes {
			if !filter.OpSuitsType(s.Op, field.Type) {
				continue
			}
			ps := tf.schema(field)
			if s.Op == filter.OpLike {
				ps = map[string]any{"type": "string"}
			}
			if s.Op == filter.OpIn {
				ps = map[string]any{"type": "array", "items": tf.schema(field), "minItems": 1}
			}
			ps["description"] = fmt.Sprintf("Filter by %s: %s.", field.Name, mcpOperatorLabel(s.Op))
			props[spell.Key(mcpFieldKey(field), s.Op)] = ps
		}
	}
	return map[string]any{"type": "object", "properties": props}
}

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

func writeToolSchema(ent *entity.Entity, reg entity.Registry) map[string]any {
	return bodyToolSchema(ent, reg, false)
}

func updateToolSchema(ent *entity.Entity, reg entity.Registry, args []string) map[string]any {
	out := bodyToolSchema(ent, reg, true)
	props := out["properties"].(map[string]any)
	tf := toolFieldsOf(reg, ent)
	for _, a := range args {
		props[a] = keyArgSchema(ent, tf, a)
	}
	out["required"] = slices.Clone(args)
	return out
}

// bodyToolSchema is the create (update=false) or update tool body: every
// field a client may write, under enforced states.
func bodyToolSchema(ent *entity.Entity, reg entity.Registry, update bool) map[string]any {
	st := ent.Config.States
	guarded := st != nil && !st.Advisory
	stamps := map[string]bool{}
	stateKey := ""
	if guarded {
		for _, f := range ent.GetFields() {
			if f.Name == st.Field {
				stateKey = mcpFieldKey(f)
			}
			for _, t := range st.Transitions {
				if t.Stamp == f.Name {
					stamps[mcpFieldKey(f)] = true
				}
			}
		}
	}
	serverStamped := map[string]bool{}
	if f := ent.Config.Scope.OwnerField; f != "" {
		serverStamped[f] = true
	}
	if ent.Config.Scope.MultiTenant {
		serverStamped[ent.Config.TenantColumn()] = true
	}
	props := make(map[string]any)
	var required []string
	tf := toolFieldsOf(reg, ent)
	for _, field := range tf.fields {
		if field.AutoGenerate != schema.AutoNone || field.ReadOnly || field.Hidden || serverStamped[field.Name] {
			continue
		}
		key := mcpFieldKey(field)
		if stamps[key] {
			continue
		}
		if update && key == stateKey {
			continue
		}
		fs := tf.schema(field)
		if !update && key == stateKey {
			fs["enum"] = st.InitialValues(ent.GetFields())
		}
		if field.Required && field.Default == nil && !serverStamped[field.Name] {
			required = append(required, key)
		} else if !field.Required && key != stateKey {
			nullableToolProp(fs)
		}
		props[key] = fs
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// mcpFieldKey is a field's tool parameter name: WireName, else Name.
func mcpFieldKey(field schema.Field) string {
	if field.WireName != "" {
		return field.WireName
	}
	return field.Name
}

type toolFields struct {
	fields []schema.Field
	// pending names reference fields whose target was not registered yet.
	pending map[string]bool
}

func toolFieldsOf(reg entity.Registry, ent *entity.Entity) toolFields {
	tf := toolFields{fields: entity.ValueFields(reg, ent)}
	if reg == nil {
		return tf
	}
	for _, rel := range ent.Config.Relations {
		if rel.Type != entity.RelManyToOne || len(rel.ForeignKey) != 1 {
			continue
		}
		if target, err := entity.ResolveTarget(reg, ent, rel.Entity); err == nil && target != nil {
			continue
		}
		for _, f := range tf.fields {
			if f.Type == schema.Relation && f.RawType == "" && strings.EqualFold(f.Name, rel.ForeignKey[0]) {
				if tf.pending == nil {
					tf.pending = map[string]bool{}
				}
				tf.pending[f.Name] = true
			}
		}
	}
	return tf
}

// schema types a field's tool argument; a pending reference takes an integer or a string.
func (tf toolFields) schema(field schema.Field) map[string]any {
	if tf.pending[field.Name] {
		return map[string]any{"type": []any{"integer", "string"}}
	}
	return mcpFieldSchema(field)
}

func nullableToolProp(prop map[string]any) {
	if types, ok := prop["type"].([]any); ok {
		prop["type"] = append(slices.Clone(types), "null")
		return
	}
	coreoa.Nullable(prop)
}

func mcpFieldSchema(field schema.Field) map[string]any {
	switch field.Type {
	case schema.Int:
		return map[string]any{"type": "integer"}
	case schema.Float:
		return map[string]any{"type": "number"}
	case schema.Bool:
		return map[string]any{"type": "boolean"}
	case schema.JSON:
		return map[string]any{}
	case schema.Enum:
		return map[string]any{"type": "string", "enum": field.Values}
	default:
		return map[string]any{"type": "string"}
	}
}
