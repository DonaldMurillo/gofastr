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
	"github.com/DonaldMurillo/gofastr/internal/casing"
)

// EntityOpenAPI builds the spec for every registered entity, with an
// optional basePath ("/api") in every path key. crudMounted reports which
// entities' CRUD routes are mounted; nil checks Exposure.CRUD alone.
func EntityOpenAPI(registry entity.Registry, title, version string, crudMounted func(*entity.Entity) bool, basePath ...string) *openapi.Spec {
	return EntityOpenAPIWithBulk(registry, title, version, crudMounted, nil, basePath...)
}

// EntityOpenAPIWithBulk is EntityOpenAPI that also documents the _bulk,
// _export.csv and _pick routes of each entity bulkMounted reports.
func EntityOpenAPIWithBulk(registry entity.Registry, title, version string, crudMounted, bulkMounted func(*entity.Entity) bool, basePath ...string) *openapi.Spec {
	o := Options{Title: title, Version: version, CRUDMounted: crudMounted, BulkMounted: bulkMounted}
	if len(basePath) > 0 {
		o.BasePath = basePath[0]
	}
	return EntityOpenAPIFor(registry, o)
}

// Options configures EntityOpenAPIFor.
type Options struct {
	Title, Version string
	// CRUDMounted reports an entity whose CRUD routes are mounted; nil
	// checks Exposure.CRUD.
	CRUDMounted func(*entity.Entity) bool
	// BulkMounted reports an entity with _bulk, _export.csv and _pick.
	BulkMounted func(*entity.Entity) bool
	// ReadOnlyMounted reports an entity whose writes are not mounted.
	ReadOnlyMounted func(*entity.Entity) bool
	// BasePath is the API prefix baked into every path key ("/api").
	BasePath string
	// Grouped reports an entity registered in a route group: its paths
	// join its Version (the root group's "" included), not BasePath. Nil
	// counts an entity with a Version as grouped.
	Grouped func(*entity.Entity) bool
	// JSONCase is the casing the CRUD handlers answer in.
	JSONCase crud.JSONCase
}

// EntityOpenAPIFor builds the spec as o configures it; its RequestView
// keeps only the entities the request's caller may read.
func EntityOpenAPIFor(registry entity.Registry, o Options) *openapi.Spec {
	s := entityOpenAPI(registry, o, nil)

	s.RequestView = func(r *http.Request) *openapi.Spec {
		ctx := r.Context()
		return entityOpenAPI(registry, o, func(ent *entity.Entity) bool {
			return canReadSpecEntity(ctx, ent)
		})
	}
	return s
}

func canReadSpecEntity(ctx context.Context, ent *entity.Entity) bool {
	return access.ScopeAllows(ctx, access.Permission(ent.GetTable()+":read")) &&
		(&crud.CrudHandler{Entity: ent}).CanReadScoped(ctx)
}

func objectSchemaWith(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

func listEnvelopes(s *openapi.Spec, schemaName string, entityRef map[string]any, schemaNames map[string]bool) (listRef, cursorRef map[string]any) {
	listResponse := objectSchemaWith(map[string]any{
		"data":       map[string]any{"type": "array", "items": entityRef},
		"total":      map[string]any{"type": "integer"},
		"page":       map[string]any{"type": "integer"},
		"perPage":    map[string]any{"type": "integer"},
		"totalPages": map[string]any{"type": "integer"},
	})
	listResponse["required"] = []string{"data", "total", "page", "perPage", "totalPages"}

	cursorPage := objectSchemaWith(map[string]any{
		"data":    map[string]any{"type": "array", "items": entityRef},
		"cursor":  map[string]any{"type": "string", "description": "Opaque cursor for the next page; empty when there are no more results."},
		"hasMore": map[string]any{"type": "boolean"},
		"total":   map[string]any{"type": "integer"},
	})
	cursorPage["required"] = []string{"data", "cursor", "hasMore"}

	register := func(name string, component map[string]any) map[string]any {
		if schemaNames[name] {
			return component
		}
		s.AddSchema(name, component)
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	return register(schemaName+"ListResponse", listResponse), register(schemaName+"CursorPage", cursorPage)
}

func entityOpenAPI(registry entity.Registry, o Options, keep func(*entity.Entity) bool) *openapi.Spec {
	s := openapi.NewSpec(o.Title, o.Version)
	crudMounted, bulkMounted := o.CRUDMounted, o.BulkMounted
	jc := o.JSONCase
	apiPrefix := ""
	if o.BasePath != "" && o.BasePath != "/" {
		apiPrefix = "/" + strings.Trim(o.BasePath, "/")
	}
	s.AddServer("/", "current")

	s.AddSchema("Error", objectSchemaWith(map[string]any{
		"error":         map[string]any{"type": "string"},
		"success":       map[string]any{"type": "boolean"},
		"code":          map[string]any{"type": "integer"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
	}))

	s.AddSchema("BatchResult", objectSchemaWith(map[string]any{
		"index":         map[string]any{"type": "integer"},
		"data":          map[string]any{"type": "object"},
		"error":         map[string]any{"type": "string"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"skipped":       map[string]any{"type": "boolean"},
	}))

	s.AddSchema("BatchResponse", objectSchemaWith(map[string]any{
		"committed": map[string]any{"type": "boolean"},
		"results":   map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/BatchResult"}},
	}))

	s.AddSchema("TransitionConflict", objectSchemaWith(map[string]any{
		"error":   map[string]any{"type": "string"},
		"success": map[string]any{"type": "boolean"},
		"reason":  map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"current": map[string]any{"type": "string", "description": "The record's current state value."},
		"moves":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The move keys open from the current value."},
	}))

	s.AddSchema("StateError", objectSchemaWith(map[string]any{
		"error":         map[string]any{"type": "string"},
		"success":       map[string]any{"type": "boolean"},
		"reason":        map[string]any{"type": "string", "description": "Stable code of the refusal (the catalog key's last segment); error is its text in the request's locale."},
		"fields":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"field_reasons": map[string]any{"type": "object", "description": "Stable code per entry of fields, in the same order.", "additionalProperties": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		"moves":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}))

	anyGated := false

	components := map[string]bool{}

	emitted := map[*entity.Entity]string{}

	schemaNames := map[string]bool{}
	for _, ent := range registry.AllSorted() {
		if keep != nil && !keep(ent) {
			continue
		}
		emitted[ent] = componentName(ent)
		schemaNames[componentName(ent)] = true
		if ent.Version == "" {
			components[ent.GetName()] = true
		}
	}

	for _, ent := range registry.AllSorted() {
		if keep != nil && !keep(ent) {
			continue
		}
		entityName := ent.GetName()
		tableName := ent.GetTable()
		fields := ent.GetFields()

		disambig := ""
		tagName := entityName
		pathPrefix := ""
		entPrefix := apiPrefix
		if ent.Version != "" || o.Grouped != nil && o.Grouped(ent) {
			entPrefix = ""
		}
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

		visibleFields := make([]schema.Field, 0, len(fields))
		for _, f := range fields {
			if !f.Hidden {
				visibleFields = append(visibleFields, f)
			}
		}

		wireFields := make([]schema.Field, len(visibleFields))
		for i, f := range visibleFields {
			f.Name = wireKeyOf(f, jc)
			f.WireName = ""
			wireFields[i] = f
		}
		entitySchema := openapi.FieldsToSchema(wireFields)

		wireProps := entitySchema["properties"].(map[string]any)
		for i, f := range visibleFields {
			if f.Type == schema.Relation {
				prop := wireProps[wireFields[i].Name].(map[string]any)
				typeRelationKey(prop, registry, ent, f)

				if keep != nil {
					if target, err := entity.ResolveTarget(registry, ent, f.To); err != nil || target == nil || emitted[target] == "" {
						delete(prop, "x-relation")
					}
				}
			}
		}
		keyCols := ent.KeyColumns()
		for i, f := range visibleFields {
			if !f.Required && f.AutoGenerate == schema.AutoNone && !slices.Contains(keyCols, f.Name) {
				openapi.Nullable(wireProps[wireFields[i].Name].(map[string]any))
			}
		}

		s.AddSchema(schemaName, readSchema(registry, ent, entitySchema, jc, emitted))

		crudExposed := ent.Config.Exposure.CRUD == nil || *ent.Config.Exposure.CRUD
		if crudMounted != nil {
			crudExposed = crudMounted(ent)
		}
		if !crudExposed {
			if hasCustomEndpoints(ent) {
				s.AddTag(tagName, entityName+" operations")
			}
			anyGated = addCustomEndpoints(s, ent, schemaName, tagName, entPrefix, components) || anyGated
			continue
		}

		s.AddTag(tagName, entityName+" operations")

		readOnly := o.ReadOnlyMounted != nil && o.ReadOnlyMounted(ent)
		addWrite := func(method, p string, op openapi.Operation) {
			if !readOnly {
				s.AddPath(method, p, op)
			}
		}

		entityRef := map[string]any{"$ref": "#/components/schemas/" + schemaName}
		singleRef := map[string]any{
			"type":     "object",
			"required": []string{"data"},
			"properties": map[string]any{
				"data": entityRef,
			},
		}
		listRef, cursorRef := listEnvelopes(s, schemaName, entityRef, schemaNames)
		errorRef := map[string]any{"$ref": "#/components/schemas/Error"}
		conflictRef := map[string]any{"$ref": "#/components/schemas/TransitionConflict"}
		stateErrRef := map[string]any{"$ref": "#/components/schemas/StateError"}
		batchRespRef := map[string]any{"$ref": "#/components/schemas/BatchResponse"}

		st := ent.Config.States
		statesGuarded := st != nil && !st.Advisory

		var scopeSkip []string
		if owner := ent.Config.Scope.OwnerField; owner != "" {
			scopeSkip = append(scopeSkip, owner)
		}
		if ent.Config.Scope.MultiTenant {
			scopeSkip = append(scopeSkip, ent.Config.TenantColumn())
		}

		path := entPrefix + pathPrefix + "/" + tableName
		recPath, keyProps, keyReq := recordKey(ent, path, jc)

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
			if ent.HidesRelation(rel) {
				continue
			}
			includeNames = append(includeNames, rel.Name)
		}
		includeDesc := "Comma-separated list of relations to eager-load."
		includeSchema := map[string]any{"type": "string"}
		if len(includeNames) > 0 {
			includeDesc += " Available: " + strings.Join(includeNames, ", ") + "."
		}

		fieldNames := make([]string, 0, len(visibleFields))
		for _, f := range visibleFields {
			fieldNames = append(fieldNames, wireKeyOf(f, jc))
		}
		fieldsDesc := "Comma-separated list of optional fields to return (projection); the primary key and required fields are always included."
		if len(fieldNames) > 0 {
			fieldsDesc += " Available: " + strings.Join(fieldNames, ", ") + "."
		}
		fieldsSchema := map[string]any{"type": "string"}

		listOp := openapi.NewOperation()
		listOp.Summary = "List " + entityName
		listOp.OperationID = "list_" + schemaName
		listOp.Tags = []string{tagName}
		listOp.AddParameter("page", "query", "Page number (offset mode)", false, map[string]any{"type": "integer", "default": 1})

		defLimit, maxLimit := crud.ListLimits(ent.Config.Pagination.MaxListLimit)
		listOp.AddParameter("limit", "query", fmt.Sprintf("Items per page (max %d; a larger limit is cut to it)", maxLimit), false, map[string]any{"type": "integer", "default": defLimit})

		listOp.AddParameter("per_page", "query", "Alias for limit (items per page); limit wins when both are sent, and the same max applies.", false, map[string]any{"type": "integer", "default": defLimit})

		listOp.AddParameter("offset", "query", fmt.Sprintf("Rows to skip, replacing the page-derived skip (offset mode only; ignored with ?cursor). Refused with 400 past the offset ceiling (max %d rows, or the handler's MaxOffset).", maxLimit*1000), false, map[string]any{"type": "integer", "minimum": 0})

		listOp.AddParameter("stream", "query", "Stream the response (chunked) instead of buffering it; refused with 400 when combined with ?include= or an AfterList hook.", false, map[string]any{"type": "boolean", "default": false})
		listOp.AddParameter("sort", "query", "Sort field (offset mode only; ignored when ?cursor is present)", false, map[string]any{"type": "string"})
		listOp.AddParameter("cursor", "query", "Opaque cursor; presence (even empty) switches the response to CursorPage shape and uses keyset pagination by primary key.", false, map[string]any{"type": "string"})
		listOp.AddParameter("direction", "query", "Cursor walk direction: forward (default) or backward.", false, map[string]any{"type": "string", "enum": []string{"forward", "backward"}, "default": "forward"})
		listOp.AddParameter("include", "query", includeDesc, false, includeSchema)
		listOp.AddParameter("fields", "query", fieldsDesc, false, fieldsSchema)

		if ent.Config.Scope.SoftDelete {
			listOp.AddParameter("trashed", "query", "Include soft-deleted rows (requires authentication).", false, map[string]any{"type": "boolean", "default": false})
		}

		spell := filter.SpellerFor(visibleFields)
		for _, f := range visibleFields {
			if f.NoQuery {
				continue
			}
			name := f.Name
			if f.WireName != "" {
				name = f.WireName
			}

			if f.Type == schema.Relation && f.RawType == "" && entity.RelationHoldsInt(registry, ent, f.Name) {
				f.Type = schema.Int
			}
			filterSchema := fieldToFilterSchema(f)

			for _, o := range advertisedFilterOps {
				if !filter.OpSuitsType(o.op, f.Type) {
					continue
				}
				ps := filterSchema
				if o.stringSchema {
					ps = map[string]any{"type": "string"}
				}
				desc := o.desc(name)
				if o.op == filter.OpEq && filter.IsListControl(name) && (name != "q" || len(ent.Config.SearchFields) > 0) {
					desc += " (bracket form: a bare " + name + " is the list control)"
				}
				listOp.AddParameter(spell.Key(name, o.op), "query", desc, false, ps)
			}
		}

		if len(ent.Config.SearchFields) > 0 {
			desc := "Free-text search across: " + strings.Join(ent.Config.SearchFields, ", ")
			listOp.AddParameter("q", "query", desc, false, map[string]any{"type": "string"})
		}

		listOp.AddResponse(200, "List of "+entityName, map[string]any{"oneOf": []any{listRef, cursorRef}})
		listOp.AddResponse(400, "Invalid filters or unknown include", errorRef)
		if gated {
			listOp.AddResponse(401, "Authentication required", errorRef)
			listOp.AddResponse(403, "Forbidden", errorRef)
			listOp.AddSecurity("bearerAuth", nil)
			listOp.AddSecurity("cookieAuth", nil)
		}
		s.AddPath("GET", path, *listOp)

		createOp := openapi.NewOperation()
		createOp.Summary = "Create " + entityName
		createOp.OperationID = "create_" + schemaName
		createOp.Tags = []string{tagName}

		createSchema := requestSchema(entitySchema, fields, st, scopeSkip, false, jc)
		createOp.SetRequestBody("application/json", createSchema, true)
		createOp.AddResponse(201, "Created "+entityName, singleRef)
		createOp.AddResponse(400, "Validation error", errorRef)
		createOp.AddResponse(409, "A unique value is already taken, or a relation names a record that does not exist", errorRef)
		addBodyRefusals(createOp, errorRef)
		if statesGuarded {
			createOp.AddResponse(422, "Tried to set the state field to a non-initial value, or to set a stamp", stateErrRef)
		}
		if gated {
			createOp.AddResponse(401, "Authentication required", errorRef)
			createOp.AddResponse(403, "Forbidden", errorRef)
			createOp.AddSecurity("bearerAuth", nil)
			createOp.AddSecurity("cookieAuth", nil)
		}
		addWrite("POST", path, *createOp)

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

		updateOp := openapi.NewOperation()
		updateOp.Summary = "Update " + entityName
		updateOp.OperationID = "update_" + schemaName
		updateOp.Tags = []string{tagName}

		updateOp.SetRequestBody("application/json", requestSchema(entitySchema, fields, st, scopeSkip, true, jc), false)
		updateOp.AddResponse(200, "Updated "+entityName, singleRef)
		updateOp.AddResponse(400, "Validation error", errorRef)
		updateOp.AddResponse(409, "A unique value is already taken, or a relation names a record that does not exist", errorRef)
		addBodyRefusals(updateOp, errorRef)
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
		addWrite("PUT", recPath, *updateOp)

		patchOp := openapi.NewOperation()
		patchOp.Summary = "Patch " + entityName
		patchOp.OperationID = "patch_" + schemaName
		patchOp.Tags = []string{tagName}
		patchSchema := requestSchema(entitySchema, fields, st, scopeSkip, true, jc)
		patchOp.SetRequestBody("application/json", patchSchema, true)
		patchOp.AddResponse(200, "Patched "+entityName, singleRef)
		patchOp.AddResponse(400, "Validation error", errorRef)
		patchOp.AddResponse(409, "A unique value is already taken, or a relation names a record that does not exist", errorRef)
		addBodyRefusals(patchOp, errorRef)
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
		addWrite("PATCH", recPath, *patchOp)

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
		deleteOp.AddResponse(409, "Other records still reference this one", errorRef)
		addWrite("DELETE", recPath, *deleteOp)

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
			addWrite("POST", recPath+"/transitions/"+t.Key, *moveOp)
		}

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
		addBodyRefusals(batchCreateOp, errorRef)
		if gated {
			batchCreateOp.AddResponse(401, "Authentication required", errorRef)
			batchCreateOp.AddResponse(403, "Forbidden", errorRef)
			batchCreateOp.AddSecurity("bearerAuth", nil)
			batchCreateOp.AddSecurity("cookieAuth", nil)
		}
		addWrite("POST", path+"/_batch", *batchCreateOp)

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
		addBodyRefusals(batchUpdateOp, errorRef)
		if gated {
			batchUpdateOp.AddResponse(401, "Authentication required", errorRef)
			batchUpdateOp.AddResponse(403, "Forbidden", errorRef)
			batchUpdateOp.AddSecurity("bearerAuth", nil)
			batchUpdateOp.AddSecurity("cookieAuth", nil)
		}
		addWrite("PATCH", path+"/_batch", *batchUpdateOp)

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
		addBodyRefusals(batchDeleteOp, errorRef)
		if gated {
			batchDeleteOp.AddResponse(401, "Authentication required", errorRef)
			batchDeleteOp.AddResponse(403, "Forbidden", errorRef)
			batchDeleteOp.AddSecurity("bearerAuth", nil)
			batchDeleteOp.AddSecurity("cookieAuth", nil)
		}
		addWrite("DELETE", path+"/_batch", *batchDeleteOp)
		if !readOnly && bulkMounted != nil && bulkMounted(ent) {
			addBulkPaths(s, path, entityName, schemaName, tagName, gated, errorRef)
		}

		anyGated = addCustomEndpoints(s, ent, schemaName, tagName, entPrefix, components) || anyGated
	}

	if anyGated {
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

func requestSchema(specSchema map[string]any, fields []schema.Field, st *entity.StatesConfig, scopeSkip []string, update bool, jc crud.JSONCase) map[string]any {
	cp := excludeFieldsByBehavior(specSchema, fields, jc)
	byName := make(map[string]schema.Field, len(fields))
	for _, f := range fields {
		byName[f.Name] = f
	}
	drop := map[string]bool{}
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

var advertisedFilterOps = []struct {
	suffix string
	op     filter.FilterOp
	desc   func(name string) string

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

func excludeFieldsByBehavior(specSchema map[string]any, fields []schema.Field, jc crud.JSONCase) map[string]any {
	cp := make(map[string]any, len(specSchema))
	maps.Copy(cp, specSchema)

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

func versionSlug(prefix string) string {
	if prefix == "" {
		return ""
	}
	s := strings.TrimPrefix(prefix, "/")
	return strings.ReplaceAll(s, "/", "_")
}

func wireKeyOf(f schema.Field, jc crud.JSONCase) string {
	if f.WireName != "" {
		return f.WireName
	}
	return columnKey(f.Name, jc)
}

func columnKey(col string, jc crud.JSONCase) string {
	if jc == crud.CaseSnake {
		return col
	}
	return casing.ToCamel(col)
}

func componentName(ent *entity.Entity) string {
	if ent.Version == "" {
		return ent.GetName()
	}
	return ent.GetName() + "_" + versionSlug(ent.Version)
}

func readSchema(reg entity.Registry, ent *entity.Entity, base map[string]any, jc crud.JSONCase, emitted map[*entity.Entity]string) map[string]any {
	if len(ent.Config.Relations) == 0 {
		return base
	}
	props, ok := base["properties"].(map[string]any)
	if !ok {
		return base
	}
	out := maps.Clone(base)
	props = maps.Clone(props)
	out["properties"] = props
	wire := map[string]string{}
	for _, f := range ent.GetFields() {
		wire[f.Name] = wireKeyOf(f, jc)
	}
	for _, rel := range ent.Config.Relations {
		if ent.HidesRelation(rel) {
			continue
		}
		key, ok := wire[rel.Name]
		if !ok {
			key = columnKey(rel.Name, jc)
		}

		record, targetName := map[string]any{"type": "object"}, ""
		if target, err := entity.ResolveTarget(reg, ent, rel.Entity); err == nil && target != nil {
			if name, ok := emitted[target]; ok {
				record = map[string]any{"$ref": "#/components/schemas/" + name}
				targetName = rel.Entity + " "
			}
		}
		desc := "With ?include=" + rel.Name + ": the related " + targetName + "record"
		var loaded map[string]any
		switch rel.Type {
		case entity.RelHasMany, entity.RelManyToMany:
			loaded = map[string]any{"type": "array", "items": record, "description": desc + "s."}
		default:
			loaded = map[string]any{"anyOf": []any{record, map[string]any{"type": "null"}}, "description": desc + ", or null when none is readable."}
		}
		if own, ok := props[key].(map[string]any); ok {
			loaded = map[string]any{
				"anyOf":       []any{own, record, map[string]any{"type": "null"}},
				"description": "The key of the related " + targetName + "record; " + strings.ToLower(desc[:1]) + desc[1:] + " itself, or null when it is not readable.",
			}
			if x, ok := own["x-relation"]; ok {
				loaded["x-relation"] = x
			}
		}
		props[key] = loaded
	}
	return out
}

func typeRelationKey(prop map[string]any, reg entity.Registry, ent *entity.Entity, f schema.Field) {
	if prop == nil || f.RawType != "" {
		return
	}
	if !f.Many && entity.RelationHoldsInt(reg, ent, f.Name) {
		prop["type"] = "integer"
		delete(prop, "format")
		return
	}
	target, err := entity.ResolveTarget(reg, ent, f.To)
	if err != nil || target == nil {
		return
	}
	pk, err := target.KeyColumn()
	if err != nil {
		return
	}
	i := slices.IndexFunc(target.GetFields(), func(tf schema.Field) bool { return tf.Name == pk })
	if i < 0 {
		return
	}
	key := target.GetFields()[i]
	if key.Type == schema.UUID || key.Type == schema.Relation {
		return
	}
	prop["type"] = "string"
	delete(prop, "format")
	if format, ok := openapi.FieldToSchema(key)["format"]; ok && !f.Many && key.Type != schema.Text {
		prop["format"] = format
	}
}

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

func batchIDsDesc(ent *entity.Entity) string {
	cols := ent.KeyColumns()
	if len(cols) == 1 {
		return "Record ids."
	}
	return fmt.Sprintf("Record ids. An id joins the record's %s values, each path-escaped, with \"/\".", strings.Join(cols, ", "))
}

func hasCustomEndpoints(ent *entity.Entity) bool {
	for _, endpoint := range ent.Config.Endpoints {
		if EntityEndpointMethod(endpoint.Method) != "" && endpoint.Path != "" {
			return true
		}
	}
	return false
}

func addBodyRefusals(op *openapi.Operation, errorRef map[string]any) {
	op.AddResponse(413, "Request body too large", errorRef)
	op.AddResponse(415, "Content-Type must be application/json", errorRef)
}

func addCustomEndpoints(s *openapi.Spec, ent *entity.Entity, schemaName, tagName, apiPrefix string, components map[string]bool) (gated bool) {
	errorRef := map[string]any{"$ref": "#/components/schemas/Error"}
	for _, endpoint := range ent.Config.Endpoints {
		method := EntityEndpointMethod(endpoint.Method)
		if method == "" || endpoint.Path == "" {
			continue
		}
		customOp := openapi.NewOperation()
		customOp.Summary = endpoint.Description
		if customOp.Summary == "" {
			customOp.Summary = method + " " + endpoint.Path
		}
		customOp.OperationID = DefaultEndpointToolName(schemaName, method, EntityEndpointPath(ent, endpoint.Path))
		customOp.Tags = []string{tagName}

		pathParams := map[string]bool{}
		for seg := range strings.SplitSeq(EntityEndpointPath(ent, endpoint.Path), "/") {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				pathParams[seg[1:len(seg)-1]] = true
			}
		}
		inQuery := endpoint.SDK && (method == "GET" || method == "DELETE")
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
		if len(body) > 0 && method != "GET" && method != "HEAD" {
			customOp.SetRequestBody("application/json", openapi.FieldsToSchema(body), true)
		}

		if !endpoint.Public {
			customOp.AddSecurity("bearerAuth", nil)
			customOp.AddSecurity("cookieAuth", nil)
			customOp.AddResponse(401, "Authentication required", errorRef)
			gated = true
		}

		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			customOp.AddResponse(403, "A cross-site form post", errorRef)
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
		s.AddPath(method, EntityEndpointRoutePath(ent, endpoint.Path, apiPrefix), *customOp)
	}
	return gated
}
