package openapi

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// EntityOpenAPI generates a full OpenAPI Spec from all registered entities.
// It produces:
//   - Schema components for each entity with typed fields
//   - CRUD paths (GET, POST, PUT, PATCH, DELETE) with request/response schemas
//   - List endpoint with pagination parameters
//   - Proper error response schemas
//
// EntityOpenAPI builds the spec for every registered entity. An optional
// basePath (e.g. "/api", from AppConfig.APIPrefix) is baked into the per-path
// keys, so a documented path IS the path a client requests ("/api/posts"), and
// the single server entry stays "/".
//
// This used to be the other way round: relative path keys ("/posts") plus a
// servers[0].url of "/api". That composes to the same URL and is legal
// OpenAPI, but it is the one form that misleads a reader who takes `paths`
// literally, and that reader is this framework's primary audience. The
// 2026-07-26 backend eval reproduced the confusion twice, in both its agent
// and its deterministic grader, and ranked fixing it as the most valuable
// change available. Repeating the prefix in `servers` as well would double it
// to /api/api/posts for any client that composes the two, so it does not.
//
// crudMounted reports whether an entity's auto-CRUD routes were actually
// registered. The app passes its route predicate (DB attached AND
// Exposure.CRUD), so the spec cannot advertise CRUD paths registration
// never mounted — the Exposure flag alone misses the no-DB case (#266).
// nil falls back to the Exposure-only check. Declared custom Endpoints
// are documented either way; App mounts those outside its CRUD branch.
func EntityOpenAPI(registry entity.Registry, title, version string, crudMounted func(*entity.Entity) bool, basePath ...string) *openapi.Spec {
	return EntityOpenAPIWithBulk(registry, title, version, crudMounted, nil, basePath...)
}

// EntityOpenAPIWithBulk is EntityOpenAPI that also documents the routes
// App.EntityUI mounts beside an entity's CRUD routes, POST <path>/_bulk
// and GET <path>/_export.csv, for each entity bulkMounted reports. nil
// documents neither, which is EntityOpenAPI.
func EntityOpenAPIWithBulk(registry entity.Registry, title, version string, crudMounted, bulkMounted func(*entity.Entity) bool, basePath ...string) *openapi.Spec {
	o := Options{Title: title, Version: version, CRUDMounted: crudMounted, BulkMounted: bulkMounted}
	if len(basePath) > 0 {
		o.BasePath = basePath[0]
	}
	return EntityOpenAPIFor(registry, o)
}

// Options configures EntityOpenAPIFor. The zero value documents a
// camelCase API with no prefix, every CRUD route Exposure.CRUD allows,
// and no bulk routes.
type Options struct {
	Title, Version string
	// CRUDMounted reports whether an entity's auto-CRUD routes were
	// registered (see EntityOpenAPI); nil checks Exposure.CRUD alone.
	CRUDMounted func(*entity.Entity) bool
	// BulkMounted reports the entities App.EntityUI mounts _bulk,
	// _export.csv and _pick for; nil documents none.
	BulkMounted func(*entity.Entity) bool
	// BasePath is the API prefix baked into every path key ("/api").
	BasePath string
	// JSONCase is the casing the CRUD handlers answer in, the app's
	// AppConfig.JSONCase. crud.CaseSnake documents each field under its
	// column name, as the handlers send it; empty or crud.CaseCamel
	// camelCases it. A WireName wins either way.
	JSONCase crud.JSONCase
}

// EntityOpenAPIFor is EntityOpenAPIWithBulk configured by o. App builds
// its /openapi.json with it, passing its JSONCase so a snake_case app's
// document names the keys its routes send.
func EntityOpenAPIFor(registry entity.Registry, o Options) *openapi.Spec {
	s := entityOpenAPI(registry, o, nil)
	// The auth-gated serving path (core/openapi Handler) rebuilds the
	// document per request through RequestView, keeping only the
	// entities THIS caller can read — llm.md parity: the spec is the
	// same disclosure class as the llm.md index, and an authenticated
	// caller with no read grant for a gated entity gets 403 on its rows,
	// so its name, paths, and columns must not reach them via
	// /openapi.json either (2026-09-05 red-probe round). The public
	// opt-in (WithPublicOpenAPI → PublicHandler) ignores RequestView
	// and keeps serving the full spec.
	s.RequestView = func(r *http.Request) *openapi.Spec {
		ctx := r.Context()
		return entityOpenAPI(registry, o, func(ent *entity.Entity) bool {
			return canReadSpecEntity(ctx, ent)
		})
	}
	return s
}

// canReadSpecEntity reports whether ctx passes the read-scope gates the
// entity's own List route runs, as a boolean: the crud predicate
// CanReadScoped (baseline session requirement, owner scoping, tenant
// scoping, RBAC — the same bundle llm.md's canListEntity assembles
// in-process). An entity this caller would only ever see 401/403 on is
// omitted from their spec document entirely; the spec is the disclosure.
// A scoped token also needs "<table>:read", which auth.RequireAPIScopes
// demands on the List route: CanReadScoped passes an entity with no read
// permission without reading held scopes.
func canReadSpecEntity(ctx context.Context, ent *entity.Entity) bool {
	return access.ScopeAllows(ctx, access.Permission(ent.GetTable()+":read")) &&
		(&crud.CrudHandler{Entity: ent}).CanReadScoped(ctx)
}

// objectSchemaWith wraps a property map in the OpenAPI object envelope
// ("type": "object" + "properties") shared by every fixed schema this
// document registers (Error, ListResponse, CursorPage, BatchResult,
// BatchResponse). It replaces the five former inline envelope copies in
// entityOpenAPI. (Distinct from endpoint.go's objectSchema, the bare
// `{"type": "object"}` fallback for untyped endpoints.)
func objectSchemaWith(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

// entityOpenAPI is EntityOpenAPI's builder: keep, when non-nil, filters
// which registered entities reach the document.
func entityOpenAPI(registry entity.Registry, o Options, keep func(*entity.Entity) bool) *openapi.Spec {
	s := openapi.NewSpec(o.Title, o.Version)
	crudMounted, bulkMounted := o.CRUDMounted, o.BulkMounted
	jc := o.JSONCase
	apiPrefix := ""
	if o.BasePath != "" && o.BasePath != "/" {
		apiPrefix = "/" + strings.Trim(o.BasePath, "/")
	}
	s.AddServer("/", "current")

	// Add common error response schema
	s.AddSchema("Error", objectSchemaWith(map[string]any{
		"error":         map[string]any{"type": "string"},
		"success":       map[string]any{"type": "boolean"},
		"code":          map[string]any{"type": "integer"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
	}))

	// The list route answers with one of the two envelopes below, as a
	// oneOf. oneOf needs exactly one match, and both envelopes stay open
	// to extra keys, so each requires the keys the other never writes:
	// page/perPage/totalPages, or cursor/hasMore.

	// Offset-mode list envelope
	listResponse := objectSchemaWith(map[string]any{
		"data":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"total":      map[string]any{"type": "integer"},
		"page":       map[string]any{"type": "integer"},
		"perPage":    map[string]any{"type": "integer"},
		"totalPages": map[string]any{"type": "integer"},
	})
	listResponse["required"] = []string{"data", "total", "page", "perPage", "totalPages"}
	s.AddSchema("ListResponse", listResponse)

	// Cursor-mode list envelope (returned when ?cursor= is present). The
	// cursor path never counts rows and omits total, so it is optional.
	cursorPage := objectSchemaWith(map[string]any{
		"data":    map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"cursor":  map[string]any{"type": "string", "description": "Opaque cursor for the next page; empty when there are no more results."},
		"hasMore": map[string]any{"type": "boolean"},
		"total":   map[string]any{"type": "integer"},
	})
	cursorPage["required"] = []string{"data", "cursor", "hasMore"}
	s.AddSchema("CursorPage", cursorPage)

	// Per-item shape inside a _batch response
	s.AddSchema("BatchResult", objectSchemaWith(map[string]any{
		"index":         map[string]any{"type": "integer"},
		"data":          map[string]any{"type": "object"},
		"error":         map[string]any{"type": "string"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"skipped":       map[string]any{"type": "boolean"},
	}))

	// Top-level shape for every _batch response
	s.AddSchema("BatchResponse", objectSchemaWith(map[string]any{
		"committed": map[string]any{"type": "boolean"},
		"results":   map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/BatchResult"}},
	}))

	// 409 body of a transition whose From does not hold the record's
	// current value: the current value and the moves open from it.
	s.AddSchema("TransitionConflict", objectSchemaWith(map[string]any{
		"error":   map[string]any{"type": "string"},
		"success": map[string]any{"type": "boolean"},
		"reason":  map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"current": map[string]any{"type": "string", "description": "The record's current state value."},
		"moves":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The move keys open from the current value."},
	}))

	// 422 body of a create or update that tries to change the state field
	// or a stamp outside a move: the field error in the validation shape,
	// plus the moves the caller can run instead.
	s.AddSchema("StateError", objectSchemaWith(map[string]any{
		"error":         map[string]any{"type": "string"},
		"success":       map[string]any{"type": "boolean"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"moves":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}))

	// Track whether any entity is auth-gated so the shared security
	// schemes are registered once after the loop, not per entity.
	anyGated := false
	// components names every unversioned entity this document emits a
	// schema for, so an endpoint's Returns can reference it.
	components := map[string]bool{}
	for _, ent := range registry.AllSorted() {
		if (keep == nil || keep(ent)) && ent.Version == "" {
			components[ent.GetName()] = true
		}
	}
	// Generate schema + paths for each entity. Use AllSorted so the
	// emitted /openapi.json bytes are stable across restarts,
	// otherwise the tag array order tracks Go's randomised map
	// iteration, breaking ETag caching and golden-file diffs.
	for _, ent := range registry.AllSorted() {
		if keep != nil && !keep(ent) {
			continue // unreadable for this caller: name, paths, and columns stay out of the document
		}
		entityName := ent.GetName()
		tableName := ent.GetTable()
		fields := ent.GetFields()

		// Version-aware disambiguation. Unversioned entities (App.Entity)
		// keep the historical bare names, schema "posts", tag "posts",
		// path "/posts". Versioned entities (App.GroupEntity) carry the
		// group prefix as Version; we derive a slug from it so two versions
		// of the same entity produce non-colliding schema component names,
		// operation IDs, tags, and paths.
		disambig := ""
		tagName := entityName
		pathPrefix := ""
		if ent.Version != "" {
			disambig = "_" + versionSlug(ent.Version)
			pathPrefix = ent.Version
			if ent.OpenAPITag != "" {
				tagName = ent.OpenAPITag
			} else {
				tagName = entityName + disambig
			}
		}
		schemaName := entityName + disambig

		// Generate entity schema (excluding hidden fields from response)
		visibleFields := make([]schema.Field, 0, len(fields))
		for _, f := range fields {
			if !f.Hidden {
				visibleFields = append(visibleFields, f)
			}
		}
		// The entity layer owns default casing; the core generator also honors
		// explicit WireName. Resolve each key once before generation so an alias
		// is never looked up again under its database name or camel-cased twice.
		wireFields := make([]schema.Field, len(visibleFields))
		for i, f := range visibleFields {
			f.Name = wireKeyOf(f, jc)
			f.WireName = ""
			wireFields[i] = f
		}
		entitySchema := openapi.FieldsToSchema(wireFields)
		// The route stores a null for an optional field, so reads return
		// it. A generated value and a key column are never null.
		wireProps := entitySchema["properties"].(map[string]any)
		keyCols := ent.KeyColumns()
		for i, f := range visibleFields {
			if !f.Required && f.AutoGenerate == schema.AutoNone && !slices.Contains(keyCols, f.Name) {
				openapi.Nullable(wireProps[wireFields[i].Name].(map[string]any))
			}
		}
		s.AddSchema(schemaName, entitySchema)

		// The router mounts auto-CRUD only when Exposure.CRUD is unset
		// ("auto" ⇒ on) or explicitly true, so an entity that opted out
		// answers 404 on every generated path. Advertising them anyway
		// documents an API the server does not have, and an SDK built
		// from the spec ships methods that cannot work, worst exactly
		// where the opt-out was deliberate (sensitive rows, or invariants
		// owned by a server-side workflow).
		//
		// Only the GENERATED surface goes. The schema component above
		// stays, and so do hand-written Endpoints: App mounts those
		// outside its crudEnabled branch, so the server serves them
		// either way, and an entity that opted out of auto-CRUD is the
		// one most likely to carry them, opting out is how an app says
		// "reach this through the endpoints I declared". The tag is
		// emitted only when something will carry it; an entity with
		// neither CRUD nor endpoints would otherwise render as an empty
		// group.
		crudExposed := ent.Config.Exposure.CRUD == nil || *ent.Config.Exposure.CRUD
		if crudMounted != nil {
			crudExposed = crudMounted(ent)
		}
		if !crudExposed {
			if hasCustomEndpoints(ent) {
				s.AddTag(tagName, entityName+" operations")
			}
			if addCustomEndpoints(s, ent, schemaName, tagName, apiPrefix, components) {
				anyGated = true
			}
			continue
		}

		// Tag for grouping
		s.AddTag(tagName, entityName+" operations")

		// Reference to entity schema
		entityRef := map[string]any{"$ref": "#/components/schemas/" + schemaName}
		singleRef := map[string]any{
			"type":     "object",
			"required": []string{"data"},
			"properties": map[string]any{
				"data": entityRef,
			},
		}
		listRef := map[string]any{"$ref": "#/components/schemas/ListResponse"}
		cursorRef := map[string]any{"$ref": "#/components/schemas/CursorPage"}
		errorRef := map[string]any{"$ref": "#/components/schemas/Error"}
		conflictRef := map[string]any{"$ref": "#/components/schemas/TransitionConflict"}
		stateErrRef := map[string]any{"$ref": "#/components/schemas/StateError"}
		batchRespRef := map[string]any{"$ref": "#/components/schemas/BatchResponse"}
		// st is the entity's states block, nil without one. statesGuarded
		// reports whether the CRUD handler enforces it: only then do the
		// request schemas drop the guarded columns and the write operations
		// answer 422. The moves are documented either way: the transition
		// route is mounted for every non-system move, Advisory or not.
		st := ent.Config.States
		statesGuarded := st != nil && !st.Advisory
		// The server sets the owner field and the MultiTenant tenant column
		// on create (InjectOwner, the context tenant) and never on update
		// (crud doUpdate skips both), so no write body lists them.
		var scopeSkip []string
		if owner := ent.Config.Scope.OwnerField; owner != "" {
			scopeSkip = append(scopeSkip, owner)
		}
		if ent.Config.Scope.MultiTenant {
			scopeSkip = append(scopeSkip, ent.Config.TenantColumn())
		}
		// apiPrefix first, then any per-entity version group: the router
		// mounts /api/v2/posts, so the document has to key it that way.
		// A versioned entity's group prefix is mounted absolutely by the
		// router, so it replaces the API prefix rather than nesting
		// under it ("/api/v1/posts", never "/api/api/v1/posts").
		path := apiPrefix + pathPrefix + "/" + tableName
		if ent.Version != "" {
			path = pathPrefix + "/" + tableName
		}
		recPath, keyProps, keyReq := recordKey(ent, path, jc)

		// Auto-CRUD is secure-by-default (issue #65): an entity requires
		// an authenticated session for every operation unless it opts
		// out via Public, or an explicit mechanism already governs it
		// (owner-scoped, tenant-scoped, or an RBAC AccessControl rule,
		// which additionally rejects authenticated-but-unpermitted
		// callers with 403). The spec must advertise 401/403 on every
		// entity except a Public one, so generated SDKs and agents don't
		// assume a plain entity (no owner_field/access declared) is
		// reachable anonymously, it isn't, unless Public: true says so.
		rbacGated := ent.Config.Exposure.Access.Read != "" ||
			ent.Config.Exposure.Access.Create != "" ||
			ent.Config.Exposure.Access.Update != "" ||
			ent.Config.Exposure.Access.Delete != ""
		gated := !ent.Config.Exposure.Public || ent.Config.Scope.OwnerField != "" || ent.Config.Scope.MultiTenant || rbacGated
		if gated {
			anyGated = true
		}

		includeNames := make([]string, 0, len(ent.Config.Relations))
		for _, rel := range ent.Config.Relations {
			includeNames = append(includeNames, rel.Name)
		}
		includeDesc := "Comma-separated list of relations to eager-load."
		includeSchema := map[string]any{"type": "string"}
		if len(includeNames) > 0 {
			includeDesc += " Available: " + strings.Join(includeNames, ", ") + "."
		}

		// ?fields= projects the response down to the named columns (plus the
		// primary key, always included). Advertise the visible field names so
		// SDK generators / agents can discover the projection surface.
		fieldNames := make([]string, 0, len(visibleFields))
		for _, f := range visibleFields {
			fieldNames = append(fieldNames, wireKeyOf(f, jc))
		}
		fieldsDesc := "Comma-separated list of fields to return (projection); the primary key is always included."
		if len(fieldNames) > 0 {
			fieldsDesc += " Available: " + strings.Join(fieldNames, ", ") + "."
		}
		fieldsSchema := map[string]any{"type": "string"}

		// --- GET /{table}: List ---
		listOp := openapi.NewOperation()
		listOp.Summary = "List " + entityName
		listOp.OperationID = "list_" + schemaName
		listOp.Tags = []string{tagName}
		listOp.AddParameter("page", "query", "Page number (offset mode)", false, map[string]any{"type": "integer", "default": 1})
		listOp.AddParameter("limit", "query", "Items per page (max 100)", false, map[string]any{"type": "integer", "default": 20})
		listOp.AddParameter("sort", "query", "Sort field (offset mode only; ignored when ?cursor is present)", false, map[string]any{"type": "string"})
		listOp.AddParameter("cursor", "query", "Opaque cursor; presence (even empty) switches the response to CursorPage shape and uses keyset pagination by primary key.", false, map[string]any{"type": "string"})
		listOp.AddParameter("direction", "query", "Cursor walk direction: forward (default) or backward.", false, map[string]any{"type": "string", "enum": []string{"forward", "backward"}, "default": "forward"})
		listOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		listOp.AddParameter("fields", "query", fieldsDesc, false, fieldsSchema)
		// ?trashed=true retrieves soft-deleted rows (authenticated callers
		// only). Only meaningful when the entity opts into soft-delete.
		if ent.Config.Scope.SoftDelete {
			listOp.AddParameter("trashed", "query", "Include soft-deleted rows (requires authentication).", false, map[string]any{"type": "boolean", "default": false})
		}

		// Add filter parameters matching the actual filter parser
		// which accepts <field>, <field>_ne, <field>_gt, <field>_gte,
		// <field>_lt, <field>_lte, <field>_like, <field>_in.
		// Filter parameters use raw field names (e.g. "created_at_gt")
		// because ParseFilters matches against the schema field names
		// directly, plus a WireName alias when one is set. The wire key
		// (WireName or raw Name, NOT camelCased) is what a client sends.
		// NoQuery fields are in the response schema but rejected by the
		// filter parser under BOTH names, so advertising them here would
		// generate SDK methods and agent calls that always 400.
		for _, f := range visibleFields {
			if f.NoQuery {
				continue
			}
			name := f.Name
			if f.WireName != "" {
				name = f.WireName
			}
			filterSchema := fieldToFilterSchema(f)
			// The advertised set is derived from filter.OpSuitsType, the same
			// type predicate CheckOpType applies to ?field_<op>= (and every
			// other filter surface), so the spec can never list an operator
			// the runtime answers 400 — or hide one it accepts. Description
			// and schema per operator are the spec's own; which types see the
			// operator is the filter package's call, one source of truth.
			for _, o := range advertisedFilterOps {
				if !filter.OpSuitsType(o.op, f.Type) {
					continue
				}
				ps := filterSchema
				if o.stringSchema {
					ps = map[string]any{"type": "string"}
				}
				listOp.AddParameter(name+o.suffix, "query", o.desc(name), false, ps)
			}
		}

		// ?q= free-text search: advertised only when the entity declares
		// SearchFields. The description names the searched columns so SDK
		// generators and API consumers know the scope.
		if len(ent.Config.SearchFields) > 0 {
			desc := "Free-text search across: " + strings.Join(ent.Config.SearchFields, ", ")
			listOp.AddParameter("q", "query", desc, false, map[string]any{"type": "string"})
		}

		// 200 is one of two envelopes, clients pick by whether they sent ?cursor.
		listOp.AddResponse(200, "List of "+entityName, map[string]any{"oneOf": []any{listRef, cursorRef}})
		listOp.AddResponse(400, "Invalid filters or unknown include", errorRef)
		if gated {
			listOp.AddResponse(401, "Authentication required", errorRef)
			listOp.AddResponse(403, "Forbidden", errorRef)
			listOp.AddSecurity("bearerAuth", nil)
			listOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("GET", path, *listOp)

		// --- POST /{table}: Create ---
		createOp := openapi.NewOperation()
		createOp.Summary = "Create " + entityName
		createOp.OperationID = "create_" + schemaName
		createOp.Tags = []string{tagName}

		// Create request body excludes auto-generated and read-only
		// fields; enforced states narrow the state field's enum to the
		// values a create may set and drop the stamps.
		createSchema := requestSchema(entitySchema, fields, st, scopeSkip, false, jc)
		createOp.SetRequestBody("application/json", createSchema, true)
		createOp.AddResponse(201, "Created "+entityName, singleRef)
		createOp.AddResponse(400, "Validation error", errorRef)
		if statesGuarded {
			createOp.AddResponse(422, "Tried to set the state field to a non-initial value, or to set a stamp", stateErrRef)
		}
		if gated {
			createOp.AddResponse(401, "Authentication required", errorRef)
			createOp.AddResponse(403, "Forbidden", errorRef)
			createOp.AddSecurity("bearerAuth", nil)
			createOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("POST", path, *createOp)

		// --- GET /{table}/:id: Get by ID ---
		getOp := openapi.NewOperation()
		getOp.Summary = "Get " + entityName + " by ID"
		getOp.OperationID = "get_" + schemaName
		getOp.Tags = []string{tagName}
		getOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		getOp.AddParameter("fields", "query", fieldsDesc, false, fieldsSchema)
		getOp.AddResponse(200, "Single "+entityName, singleRef)
		getOp.AddResponse(400, "Unknown include", errorRef)
		if gated {
			getOp.AddResponse(401, "Authentication required", errorRef)
			getOp.AddResponse(403, "Forbidden", errorRef)
			getOp.AddSecurity("bearerAuth", nil)
			getOp.AddSecurity("cookieAuth", nil)
		}
		getOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("GET", recPath, *getOp)

		// --- PUT /{table}/:id: Update ---
		updateOp := openapi.NewOperation()
		updateOp.Summary = "Update " + entityName
		updateOp.OperationID = "update_" + schemaName
		updateOp.Tags = []string{tagName}
		// An enforced states block removes the state field and the stamps
		// from the update body: they change only through a move's route.
		updateOp.SetRequestBody("application/json", requestSchema(entitySchema, fields, st, scopeSkip, true, jc), false)
		updateOp.AddResponse(200, "Updated "+entityName, singleRef)
		updateOp.AddResponse(400, "Validation error", errorRef)
		if statesGuarded {
			updateOp.AddResponse(422, "Tried to change the state field or a stamp outside a move", stateErrRef)
		}
		if gated {
			updateOp.AddResponse(401, "Authentication required", errorRef)
			updateOp.AddResponse(403, "Forbidden", errorRef)
			updateOp.AddSecurity("bearerAuth", nil)
			updateOp.AddSecurity("cookieAuth", nil)
		}
		updateOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("PUT", recPath, *updateOp)

		// --- PATCH /{table}/:id: Sparse update ---
		patchOp := openapi.NewOperation()
		patchOp.Summary = "Patch " + entityName
		patchOp.OperationID = "patch_" + schemaName
		patchOp.Tags = []string{tagName}
		patchSchema := requestSchema(entitySchema, fields, st, scopeSkip, true, jc)
		patchOp.SetRequestBody("application/json", patchSchema, true)
		patchOp.AddResponse(200, "Patched "+entityName, singleRef)
		patchOp.AddResponse(400, "Validation error", errorRef)
		if statesGuarded {
			patchOp.AddResponse(422, "Tried to change the state field or a stamp outside a move", stateErrRef)
		}
		if gated {
			patchOp.AddResponse(401, "Authentication required", errorRef)
			patchOp.AddResponse(403, "Forbidden", errorRef)
			patchOp.AddSecurity("bearerAuth", nil)
			patchOp.AddSecurity("cookieAuth", nil)
		}
		patchOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("PATCH", recPath, *patchOp)

		// --- DELETE /{table}/:id: Delete ---
		deleteOp := openapi.NewOperation()
		deleteOp.Summary = "Delete " + entityName
		deleteOp.OperationID = "delete_" + schemaName
		deleteOp.Tags = []string{tagName}
		deleteOp.Responses = map[int]map[string]any{
			204: {"description": "Deleted"},
		}
		if gated {
			deleteOp.AddResponse(401, "Authentication required", errorRef)
			deleteOp.AddResponse(403, "Forbidden", errorRef)
			deleteOp.AddSecurity("bearerAuth", nil)
			deleteOp.AddSecurity("cookieAuth", nil)
		}
		deleteOp.AddResponse(404, entityName+" not found", errorRef)
		s.AddPath("DELETE", recPath, *deleteOp)

		// --- POST /{table}/:id/transitions/{key}: one operation per move ---
		// The router mounts the transition route when States holds a
		// non-system move, and the key is a literal segment, so the spec
		// documents one operation per move — the same set
		// RoutableTransitions serves, Advisory entities included (their
		// moves stay callable). System moves have no route and appear
		// nowhere.
		for _, t := range crud.RoutableTransitions(st) {
			label := t.Label
			if label == "" {
				label = t.Key
			}
			moveOp := openapi.NewOperation()
			moveOp.Summary = strings.ToUpper(label[:1]) + label[1:] + " " + entityName
			moveOp.OperationID = t.Key + "_" + schemaName
			moveOp.Tags = []string{tagName}
			desc := fmt.Sprintf("Move %s from %s to %s", st.Field, strings.Join(t.From, " or "), t.To)
			if t.Stamp != "" {
				desc += fmt.Sprintf(", stamping %s with the server's current UTC date/time", t.Stamp)
			}
			if t.Permission != "" {
				desc += ". Requires the entity's update permission and " + t.Permission
			}
			// The route takes no payload but answers 415 unless the request
			// carries the JSON content type (its cross-site-form gate), so the
			// operation declares the body a caller must send: a required,
			// property-free JSON object. Generated clients post exactly that.
			moveOp.SetRequestBody("application/json", map[string]any{
				"type":                 "object",
				"maxProperties":        0,
				"additionalProperties": false,
			}, true)
			desc += ". Sends no payload but requires Content-Type: application/json: POST the declared empty object, {}."
			moveOp.Description = desc
			moveOp.AddResponse(200, "Moved "+entityName+" (Update's envelope)", singleRef)
			moveOp.AddResponse(403, "Forbidden", errorRef)
			moveOp.AddResponse(404, entityName+" not found, or unknown move key", errorRef)
			moveOp.AddResponse(409, "The record's current value is not one the move starts from", conflictRef)
			moveOp.AddResponse(415, "Content-Type must be application/json", errorRef)
			if gated {
				moveOp.AddResponse(401, "Authentication required", errorRef)
				moveOp.AddSecurity("bearerAuth", nil)
				moveOp.AddSecurity("cookieAuth", nil)
			}
			s.AddPath("POST", recPath+"/transitions/"+t.Key, *moveOp)
		}

		// --- POST /{table}/_batch: BatchCreate ---
		batchCreateBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":     "array",
					"maxItems": crud.MaxBatchSize,
					"items":    createSchema,
				},
			},
			"required": []string{"items"},
		}
		batchCreateOp := openapi.NewOperation()
		batchCreateOp.Summary = "Batch create " + entityName + " (atomic)"
		batchCreateOp.OperationID = "batch_create_" + schemaName
		batchCreateOp.Tags = []string{tagName}
		batchCreateOp.SetRequestBody("application/json", batchCreateBody, true)
		batchCreateOp.AddResponse(200, "All items committed", batchRespRef)
		batchCreateOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchCreateOp.AddResponse(401, "Authentication required", errorRef)
			batchCreateOp.AddResponse(403, "Forbidden", errorRef)
			batchCreateOp.AddSecurity("bearerAuth", nil)
			batchCreateOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("POST", path+"/_batch", *batchCreateOp)

		// --- PATCH /{table}/_batch: BatchUpdate ---
		// An item names its record in the body, so each key column takes
		// its field's own schema (an integer key is a JSON number there),
		// never the string a path parameter is; readOnly is dropped, since
		// the item must carry it.
		entityProps, _ := entitySchema["properties"].(map[string]any)
		itemKeys := make(map[string]any, len(keyReq))
		for _, w := range keyReq {
			itemKeys[w] = keyProps[w]
			if fp, ok := entityProps[w].(map[string]any); ok {
				cp := maps.Clone(fp)
				delete(cp, "readOnly")
				itemKeys[w] = cp
			}
		}
		// An item is sparse like a single PATCH: the key part below is
		// all it must carry.
		batchPatch := requestSchema(entitySchema, fields, st, scopeSkip, true, jc)
		batchUpdateItem := map[string]any{
			"allOf": []any{
				batchPatch,
				map[string]any{"type": "object", "properties": itemKeys, "required": keyReq},
			},
		}
		batchUpdateBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":     "array",
					"maxItems": crud.MaxBatchSize,
					"items":    batchUpdateItem,
				},
			},
			"required": []string{"items"},
		}
		batchUpdateOp := openapi.NewOperation()
		batchUpdateOp.Summary = "Batch update " + entityName + " (atomic)"
		batchUpdateOp.OperationID = "batch_update_" + schemaName
		batchUpdateOp.Tags = []string{tagName}
		batchUpdateOp.SetRequestBody("application/json", batchUpdateBody, true)
		batchUpdateOp.AddResponse(200, "All items committed", batchRespRef)
		batchUpdateOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchUpdateOp.AddResponse(401, "Authentication required", errorRef)
			batchUpdateOp.AddResponse(403, "Forbidden", errorRef)
			batchUpdateOp.AddSecurity("bearerAuth", nil)
			batchUpdateOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("PATCH", path+"/_batch", *batchUpdateOp)

		// --- GET /{table}/_events: SSE entity subscription stream ---
		eventsOp := openapi.NewOperation()
		eventsOp.Summary = "Subscribe to " + entityName + " events (SSE)"
		eventsOp.OperationID = "events_" + schemaName
		eventsOp.Tags = []string{tagName}
		eventsOp.Responses[200] = map[string]any{
			"description": "Server-Sent Events stream of entity.created/updated/deleted",
			"content": map[string]any{
				"text/event-stream": map[string]any{
					"schema": map[string]any{"type": "string"},
				},
			},
		}
		if gated {
			eventsOp.AddResponse(401, "Authentication required", errorRef)
			eventsOp.AddResponse(403, "Forbidden", errorRef)
			eventsOp.AddSecurity("bearerAuth", nil)
			eventsOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("GET", path+"/_events", *eventsOp)

		// --- DELETE /{table}/_batch: BatchDelete ---
		batchDeleteBody := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{
					"type":        "array",
					"maxItems":    crud.MaxBatchSize,
					"items":       map[string]any{"type": "string"},
					"description": batchIDsDesc(ent),
				},
			},
			"required": []string{"ids"},
		}
		batchDeleteOp := openapi.NewOperation()
		batchDeleteOp.Summary = "Batch delete " + entityName + " (atomic)"
		batchDeleteOp.OperationID = "batch_delete_" + schemaName
		batchDeleteOp.Tags = []string{tagName}
		batchDeleteOp.SetRequestBody("application/json", batchDeleteBody, true)
		batchDeleteOp.AddResponse(200, "All items committed", batchRespRef)
		batchDeleteOp.AddResponse(400, "Batch rolled back; see results[]", batchRespRef)
		if gated {
			batchDeleteOp.AddResponse(401, "Authentication required", errorRef)
			batchDeleteOp.AddResponse(403, "Forbidden", errorRef)
			batchDeleteOp.AddSecurity("bearerAuth", nil)
			batchDeleteOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("DELETE", path+"/_batch", *batchDeleteOp)
		if bulkMounted != nil && bulkMounted(ent) {
			addBulkPaths(s, path, entityName, schemaName, tagName, gated, errorRef)
		}

		if addCustomEndpoints(s, ent, schemaName, tagName, apiPrefix, components) {
			anyGated = true
		}
	}

	// When at least one entity is auth-gated, advertise how callers
	// authenticate so generated SDKs and agents don't treat the gated
	// endpoints as public. Both schemes are accepted per-operation; only
	// the gated operations carry a `security` block, leaving public
	// entities unmarked. This is deliberately NOT a global security
	// requirement (Spec.AddSecurityRequirement), that would hide the
	// fact that ungated entities are anonymously reachable.
	if anyGated {
		// "__Host-session" is the production default the auth battery
		// sets in battery/auth/manager.go AuthConfig.defaults()
		// (DevMode=false). DevMode flips the name to "session_id";
		// deployments overriding AuthConfig.SessionCookie should
		// replace this scheme via Spec.SetSecurityScheme("cookieAuth", ...)
		// after building the spec.
		s.SetSecurityScheme("bearerAuth", map[string]any{
			"type":         "http",
			"scheme":       "bearer",
			"bearerFormat": "JWT",
		})
		s.SetSecurityScheme("cookieAuth", map[string]any{
			"type":        "apiKey",
			"in":          "cookie",
			"name":        "__Host-session",
			"description": "Session cookie issued by the auth battery. Name shown is the production default (`__Host-session`); DevMode uses `session_id`, and deployments overriding `AuthConfig.SessionCookie` should overwrite this scheme via `Spec.SetSecurityScheme` after building the spec.",
		})
	}
	return s
}

// requestSchema returns the write-request view of the entity schema:
// excludeFieldsByBehavior, then the scope skips, then the states rules.
// Every write drops the fields named in scopeSkip (the owner and tenant
// columns, which the server sets). An update requires nothing: PUT and
// PATCH both run the sparse update (schema.ValidatePartial). With enforced states (declared
// and not Advisory) a create keeps the state field but narrows its enum
// to the values a create may set, and drops every stamp; an update drops
// the state field and the stamps entirely — they change only through a
// move's own route.
func requestSchema(specSchema map[string]any, fields []schema.Field, st *entity.StatesConfig, scopeSkip []string, update bool, jc crud.JSONCase) map[string]any {
	cp := excludeFieldsByBehavior(specSchema, fields, jc)
	byName := make(map[string]schema.Field, len(fields))
	for _, f := range fields {
		byName[f.Name] = f
	}
	drop := map[string]bool{} // wire keys no write may carry
	for _, name := range scopeSkip {
		if f, ok := byName[name]; ok {
			drop[wireKeyOf(f, jc)] = true
		}
	}
	stateKey := ""
	if st != nil && !st.Advisory {
		for _, t := range st.Transitions {
			if t.Stamp == "" {
				continue
			}
			if f, ok := byName[t.Stamp]; ok {
				drop[wireKeyOf(f, jc)] = true
			}
		}
		if f, ok := byName[st.Field]; ok {
			stateKey = wireKeyOf(f, jc)
		}
		if update && stateKey != "" {
			drop[stateKey] = true
		}
	}
	props, ok := cp["properties"].(map[string]any)
	if !ok {
		return cp
	}
	for key := range drop {
		delete(props, key)
	}
	if !update && stateKey != "" {
		if p, ok := props[stateKey].(map[string]any); ok {
			// Copy before narrowing: the property map is shared with the
			// response schema, whose enum stays the field's full value set.
			// A create refuses a null state, so the type drops null too.
			narrowed := make(map[string]any, len(p))
			maps.Copy(narrowed, p)
			narrowed["type"] = openapi.FieldToSchema(byName[st.Field])["type"]
			narrowed["enum"] = st.InitialValues(fields)
			props[stateKey] = narrowed
		}
	}
	if update {
		delete(cp, "required")
	}
	if reqs, ok := cp["required"].([]string); ok {
		kept := make([]string, 0, len(reqs))
		for _, r := range reqs {
			if !drop[r] {
				kept = append(kept, r)
			}
		}
		if len(kept) > 0 {
			cp["required"] = kept
		} else {
			delete(cp, "required")
		}
	}
	return cp
}

// advertisedFilterOps is the spec's operator table: every
// <field><suffix> query parameter the List operation describes, in
// emission order. Membership per field type is NOT decided here — the
// emitting loop consults filter.OpSuitsType, the one type predicate
// behind filter.CheckOpType — so the spec and the runtime cannot drift
// the way a local fieldSupportsLike once did (advertising _like on Int,
// Float, Date and Decimal columns the filter parser refuses).
var advertisedFilterOps = []struct {
	suffix string
	op     filter.FilterOp
	desc   func(name string) string
	// stringSchema selects the comma-list schema _in carries (a string
	// whatever the column type); every other operator reuses the
	// column's own filterSchema.
	stringSchema bool
}{
	{"", filter.OpEq, func(n string) string { return "Exact match on " + n }, false},
	{"_ne", filter.OpNe, func(n string) string { return n + " not equal (NULL matches neither = nor !=)" }, false},
	{"_gt", filter.OpGt, func(n string) string { return n + " greater than" }, false},
	{"_gte", filter.OpGte, func(n string) string { return n + " greater than or equal" }, false},
	{"_lt", filter.OpLt, func(n string) string { return n + " less than" }, false},
	{"_lte", filter.OpLte, func(n string) string { return n + " less than or equal" }, false},
	{"_like", filter.OpLike, func(n string) string { return n + " contains (LIKE)" }, false},
	{"_in", filter.OpIn, func(n string) string { return n + " in comma-separated list" }, true},
}

// fieldToFilterSchema returns an OpenAPI query parameter schema for filtering
// on a given field type.
func fieldToFilterSchema(f schema.Field) map[string]any {
	switch f.Type {
	case schema.String, schema.Text:
		return map[string]any{"type": "string"}
	case schema.Int:
		return map[string]any{"type": "integer"}
	case schema.Float:
		return map[string]any{"type": "number"}
	case schema.Bool:
		return map[string]any{"type": "boolean"}
	case schema.Enum:
		return map[string]any{"type": "string"}
	default:
		return map[string]any{"type": "string"}
	}
}

// excludeFieldsByBehavior returns a copy of an OpenAPI schema excluding
// auto-generated, read-only, and hidden fields. A required field with a
// default leaves the required list: a write may omit it and take the
// default (schema.ValidateAll skips it). The response schema keeps it
// required, since every stored row carries it.
func excludeFieldsByBehavior(specSchema map[string]any, fields []schema.Field, jc crud.JSONCase) map[string]any {
	cp := make(map[string]any, len(specSchema))
	maps.Copy(cp, specSchema)

	// Collect field names to exclude
	exclude := make(map[string]bool)
	for _, f := range fields {
		if f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
			exclude[wireKeyOf(f, jc)] = true
		}
	}

	if props, ok := cp["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for k, v := range props {
			if !exclude[k] {
				newProps[k] = v
			}
		}
		cp["properties"] = newProps
	}
	defaulted := make(map[string]bool)
	for _, f := range fields {
		if f.Default != nil {
			defaulted[wireKeyOf(f, jc)] = true
		}
	}
	if reqs, ok := cp["required"].([]string); ok {
		filtered := make([]string, 0, len(reqs))
		for _, r := range reqs {
			if !exclude[r] && !defaulted[r] {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			cp["required"] = filtered
		} else {
			delete(cp, "required")
		}
	}
	return cp
}

// versionSlug converts a route-group prefix (e.g. "/api/v1") into an
// OpenAPI-component-safe slug (e.g. "api_v1"): strip the leading slash,
// replace remaining slashes with underscores. The result is used to
// disambiguate schema component names, operation IDs, and tags across
// versions of the same entity. Empty input yields empty output.
func versionSlug(prefix string) string {
	if prefix == "" {
		return ""
	}
	s := strings.TrimPrefix(prefix, "/")
	return strings.ReplaceAll(s, "/", "_")
}

// wireKeyOf returns the JSON wire key for a schema field under the
// handlers' casing jc: WireName verbatim when set, else the DB column
// name (crud.CaseSnake) or its camelCase (anything else), which is what
// crud's convertKey answers for the same case. This is the single
// wire-key resolution used by every OpenAPI surface that names a field,
// response properties, the required list, the ?fields= projection
// description, and excludeFieldsByBehavior (request-schema exclusion).
// Routing every surface through one function prevents the class of bug
// where one surface advertises bodyText (camelCase of Name) while the
// runtime accepts content (the WireName the entity actually exposed).
//
// Filter and sort parameters are a DELIBERATE exception: the filter
// parser (framework/filter) matches against the raw DB column name plus
// a WireName alias, never the camelCase form, so those params use
// f.Name / f.WireName directly rather than this helper.
func wireKeyOf(f schema.Field, jc crud.JSONCase) string {
	if f.WireName != "" {
		return f.WireName
	}
	return columnKey(f.Name, jc)
}

// columnKey is a column's wire key under jc when no WireName applies.
func columnKey(col string, jc crud.JSONCase) string {
	if jc == crud.CaseSnake {
		return col
	}
	return casing.ToCamel(col)
}

// recordKey returns the path naming one record of ent under path, and
// the properties and required list a batch-update item names it with.
// A one-column key is the path's {id} and the item's key column; a
// composite key takes one path segment per column (crud.KeyPath), each
// named after its column's wire key.
func recordKey(ent *entity.Entity, path string, jc crud.JSONCase) (string, map[string]any, []string) {
	cols := ent.KeyColumns()
	props := make(map[string]any, len(cols))
	req := make([]string, 0, len(cols))
	rec := path
	for _, c := range cols {
		w := columnKey(c, jc)
		for _, f := range ent.GetFields() {
			if f.Name == c {
				w = wireKeyOf(f, jc)
			}
		}
		props[w] = map[string]any{"type": "string"}
		req = append(req, w)
		rec += "/:" + w
	}
	if len(cols) == 1 {
		rec = path + "/:id"
	}
	return rec, props, req
}

// batchIDsDesc describes the ids a batch delete names its records by.
func batchIDsDesc(ent *entity.Entity) string {
	cols := ent.KeyColumns()
	if len(cols) == 1 {
		return "Record ids."
	}
	return fmt.Sprintf("Record ids. An id joins the record's %s values, each path-escaped, with \"/\".", strings.Join(cols, ", "))
}

// hasCustomEndpoints reports whether the entity declares at least one
// well-formed Endpoint, one that addCustomEndpoints will actually emit.
func hasCustomEndpoints(ent *entity.Entity) bool {
	for _, endpoint := range ent.Config.Endpoints {
		if endpoint.Method != "" && endpoint.Path != "" {
			return true
		}
	}
	return false
}

// addCustomEndpoints emits an entity's hand-written Endpoints. Split out
// of the main loop because it runs on both sides of the auto-CRUD
// opt-out: the router mounts these endpoints regardless of
// Exposure.CRUD, so the spec advertises them regardless too. It reports
// whether any operation it emitted requires a caller.
func addCustomEndpoints(s *openapi.Spec, ent *entity.Entity, schemaName, tagName, apiPrefix string, components map[string]bool) bool {
	gated := false
	for _, endpoint := range ent.Config.Endpoints {
		if endpoint.Method == "" || endpoint.Path == "" {
			continue
		}
		customOp := openapi.NewOperation()
		customOp.Summary = endpoint.Description
		if customOp.Summary == "" {
			customOp.Summary = endpoint.Method + " " + endpoint.Path
		}
		customOp.OperationID = DefaultEndpointToolName(schemaName, endpoint.Method, EntityEndpointPath(ent, endpoint.Path))
		customOp.Tags = []string{tagName}
		// A typed InputSchema splits by where each field travels. A field
		// named by a {param} segment is a path parameter, typed from its
		// field. A settings-declared endpoint (SDK) on GET or DELETE reads
		// the rest from the query string, as its generated clients send
		// it. Everything else is the JSON body, for methods that carry one
		// (GET/HEAD never do). Unset schemas fall back to the shapeless
		// {type:object} response and no body.
		pathParams := map[string]bool{}
		for seg := range strings.SplitSeq(endpoint.Path, "/") {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				pathParams[seg[1:len(seg)-1]] = true
			}
		}
		inQuery := endpoint.SDK && (endpoint.Method == "GET" || endpoint.Method == "DELETE")
		var body []schema.Field
		for _, f := range endpoint.InputSchema {
			switch {
			case pathParams[f.Name]:
				customOp.AddParameter(f.Name, "path", "", true, openapi.FieldToSchema(f))
			case inQuery:
				customOp.AddParameter(f.Name, "query", "", f.Required, openapi.FieldToSchema(f))
			default:
				body = append(body, f)
			}
		}
		if len(body) > 0 && endpoint.Method != "GET" && endpoint.Method != "HEAD" {
			customOp.SetRequestBody("application/json", openapi.FieldsToSchema(body), true)
		}
		// The route answers 401 without a user unless the endpoint is
		// Public (App.endpointRoute), so the operation says so.
		if !endpoint.Public {
			customOp.AddSecurity("bearerAuth", nil)
			customOp.AddSecurity("cookieAuth", nil)
			gated = true
		}
		out := EndpointOutputSchema(endpoint)
		if len(endpoint.OutputSchema) == 0 {
			name, list := strings.CutPrefix(endpoint.Returns, "[]")
			if components[name] {
				out = map[string]any{"$ref": "#/components/schemas/" + name}
				if list {
					out = map[string]any{"type": "array", "items": out}
				}
			}
		}
		customOp.AddResponse(200, "OK", out)
		s.AddPath(endpoint.Method, EntityEndpointRoutePath(ent, endpoint.Path, apiPrefix), *customOp)
	}
	return gated
}
