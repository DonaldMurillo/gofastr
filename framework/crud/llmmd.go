package crud

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// LLMMDOptions controls what EntityLLMMD documents. The zero value keeps
// the historical full-CRUD document; ReadOnly narrows it to the routes a
// CrudRouteOptions{ReadOnly: true} mount actually serves.
type LLMMDOptions struct {
	// ReadOnly omits every write endpoint (POST, PUT, PATCH, DELETE and
	// the _batch family) plus the per-field Create/Update columns: on a
	// read-only mount those routes answer 404/405, and the doc must not
	// advertise routes the server does not serve (#358, the #266 class
	// one layer down: the answer lives in the mount options, not the
	// entity declaration).
	ReadOnly bool
}

// EntityLLMMD generates an LLM-friendly markdown document describing all
// CRUD endpoints for a single entity. The output is designed to be
// immediately useful as context for an LLM agent, concise, structured,
// and example-rich.
//
// The document covers:
//   - Resource overview (entity name, table, primary key)
//   - List endpoint with filter operators, pagination (offset + cursor), includes
//   - Get by ID with includes
//   - Create with required/writable fields
//   - Update with writable fields
//   - Delete
//   - Batch create / update / delete
//   - SSE event stream
//   - Custom endpoints declared on the entity
//
// Pass LLMMDOptions{ReadOnly: true} for an entity mounted with
// CrudRouteOptions{ReadOnly: true} (App.View does): the write, batch and
// Create/Update-column sections are omitted so the doc matches the mount.
func EntityLLMMD(ent *entity.Entity, opts ...LLMMDOptions) string {
	var opt LLMMDOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	var b strings.Builder
	name := ent.GetName()
	table := ent.GetTable()
	fields := ent.GetFields()

	fmt.Fprintf(&b, "# %s\n\n", name)
	resourcePath := "/" + table
	if ent.Version != "" {
		resourcePath = ent.Version + resourcePath
	}
	fmt.Fprintf(&b, "Resource: `%s`\n\n", resourcePath)
	if opt.ReadOnly {
		b.WriteString("This resource is served read-only: only the list, get-by-ID and `_events` endpoints are mounted. There are no create, update, delete or batch routes.\n\n")
	}

	// --- Field reference ---
	// Read-only mounts have no create/update endpoints, so the per-field
	// Create/Update columns (which describe those endpoints' request
	// bodies) are omitted rather than frozen at "—".
	b.WriteString("## Fields\n\n")
	if opt.ReadOnly {
		b.WriteString("| Field | Type | Notes |\n")
		b.WriteString("|-------|------|-------|\n")
	} else {
		b.WriteString("| Field | Type | Create | Update | Notes |\n")
		b.WriteString("|-------|------|--------|--------|-------|\n")
	}
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		notes := ""
		if f.Unique {
			notes = "unique"
		}
		if f.Default != nil {
			if notes != "" {
				notes += ", "
			}
			notes += "default: " + sanitizeDefault(f.Default)
		}
		if len(f.Values) > 0 {
			if notes != "" {
				notes += ", "
			}
			notes += "values: " + strings.Join(f.Values, "|")
		}
		if f.NoQuery {
			if notes != "" {
				notes += ", "
			}
			notes += "not filterable/sortable"
		}
		if opt.ReadOnly {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", f.Name, fieldTypeLabel(f.Type), notes)
			continue
		}
		createCol := "—"
		updateCol := "—"
		if f.AutoGenerate == schema.AutoNone && !f.ReadOnly {
			if f.Required && f.Default == nil {
				createCol = "**required**"
			} else {
				createCol = "optional"
			}
			updateCol = "optional"
		}
		if f.AutoGenerate != schema.AutoNone {
			createCol = "auto"
			updateCol = "auto"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", f.Name, fieldTypeLabel(f.Type), createCol, updateCol, notes)
	}
	b.WriteString("\n")

	// --- Relations / Includes ---
	if len(ent.Config.Relations) > 0 {
		b.WriteString("## Includes\n\n")
		b.WriteString("Use `?include=relationName` to eager-load related data. Separate multiple with commas. Use dots for nested includes.\n\n")
		b.WriteString("| Name | Type | Target | Key |\n")
		b.WriteString("|------|------|--------|-----|\n")
		for _, rel := range ent.Config.Relations {
			fmt.Fprintf(&b, "| `%s` | %s | `%s` | `%s` |\n", rel.Name, relationTypeLabel(rel.Type), rel.Entity, rel.ForeignKey)
		}
		b.WriteString("\n")
		b.WriteString("Examples:\n")
		fmt.Fprintf(&b, "- `?include=%s`\n", ent.Config.Relations[0].Name)
		if len(ent.Config.Relations) > 1 {
			fmt.Fprintf(&b, "- `?include=%s,%s`\n", ent.Config.Relations[0].Name, ent.Config.Relations[1].Name)
		}
		// Show scoped filter example if HasMany/ManyToMany
		for _, rel := range ent.Config.Relations {
			if rel.Type == entity.RelHasMany || rel.Type == entity.RelManyToMany {
				fmt.Fprintf(&b, "- `?include=%s(status=published)`: scoped eager-load\n", rel.Name)
				break
			}
		}
		b.WriteString("\n")
	}

	// --- Endpoints ---
	b.WriteString("## Endpoints\n\n")

	// GET /{table}: List
	fmt.Fprintf(&b, "### GET %s\n\n", resourcePath)
	b.WriteString("List records with optional filtering, sorting, and pagination.\n\n")
	b.WriteString("**Query parameters:**\n\n")
	b.WriteString("| Parameter | Type | Description |\n")
	b.WriteString("|-----------|------|-------------|\n")
	b.WriteString("| `page` | integer | Page number (offset mode, default 1) |\n")
	// The cap is per entity, and hardcoding 100 here told every agent the
	// wrong number for any entity that set Pagination.MaxListLimit — the
	// same reality-drift as advertising unmounted routes, one column over.
	// listLimitCap is the function the request path actually clamps with, so
	// the doc and the clamp cannot disagree.
	fmt.Fprintf(&b, "| `limit` | integer | Items per page (default 20, max %d) |\n",
		listLimitCap(ent.Config.Pagination.MaxListLimit))
	b.WriteString("| `sort` | string | Sort field (prefix with `-` for descending, e.g. `-created_at`) |\n")
	b.WriteString("| `cursor` | string | Opaque cursor for keyset pagination. Presence switches to cursor mode. |\n")
	b.WriteString("| `direction` | string | Cursor walk direction: `forward` (default) or `backward` |\n")
	b.WriteString("| `include` | string | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")

	// Filter operators. NoQuery fields are excluded along with Hidden ones:
	// this section tells an agent what it can filter on, and a NoQuery field
	// answers every filter with a 400.
	visibleFields := make([]schema.Field, 0, len(fields))
	for _, f := range fields {
		if !f.Hidden && !f.NoQuery {
			visibleFields = append(visibleFields, f)
		}
	}
	if len(visibleFields) > 0 {
		b.WriteString("**Filter operators** (append to any filterable field name):\n\n")
		b.WriteString("| Suffix | Operator | Example |\n")
		b.WriteString("|--------|----------|----------|\n")
		sampleField := visibleFields[0].Name
		fmt.Fprintf(&b, "| (none) | equals | `%s=active` |\n", sampleField)
		fmt.Fprintf(&b, "| `_gt` | greater than | `%s_gt=100` |\n", sampleField)
		fmt.Fprintf(&b, "| `_gte` | greater than or equal | `%s_gte=100` |\n", sampleField)
		fmt.Fprintf(&b, "| `_lt` | less than | `%s_lt=100` |\n", sampleField)
		fmt.Fprintf(&b, "| `_lte` | less than or equal | `%s_lte=100` |\n", sampleField)
		fmt.Fprintf(&b, "| `_like` | LIKE (contains) | `%s_like=%%search%%` |\n", sampleField)
		fmt.Fprintf(&b, "| `_in` | IN (comma-separated) | `%s_in=a,b,c` |\n", sampleField)
		b.WriteString("\n")
	}

	b.WriteString("**Offset response:**\n```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"data\": [...],\n")
	b.WriteString("  \"total\": 42,\n")
	b.WriteString("  \"page\": 1,\n")
	b.WriteString("  \"perPage\": 20,\n")
	b.WriteString("  \"totalPages\": 3\n")
	b.WriteString("}\n```\n\n")

	b.WriteString("**Cursor response** (when `?cursor=` is present):\n```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"data\": [...],\n")
	b.WriteString("  \"cursor\": \"opaque-string-for-next-page\",\n")
	b.WriteString("  \"hasMore\": true,\n")
	b.WriteString("  \"total\": 42\n")
	b.WriteString("}\n```\n\n")

	// GET /{table}/{id}
	fmt.Fprintf(&b, "### GET %s/{id}\n\n", resourcePath)
	b.WriteString("Retrieve a single record by ID.\n\n")
	b.WriteString("| Parameter | Location | Description |\n")
	b.WriteString("|-----------|----------|-------------|\n")
	b.WriteString("| `id` | path | Record primary key |\n")
	b.WriteString("| `include` | query | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")
	b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
	b.WriteString("**Error:** `404` if not found.\n\n")

	// Write endpoints. Guarded by the mount: a CrudRouteOptions{ReadOnly:
	// true} mount (App.View) never registers these routes, so documenting
	// them would hand an agent seven 404/405s (#358).
	if !opt.ReadOnly {

		// POST /{table}
		fmt.Fprintf(&b, "### POST %s\n\n", resourcePath)
		b.WriteString("Create a new record.\n\n")
		b.WriteString("**Request body:** JSON object with writable fields.\n```json\n")
		b.WriteString("{\n")
		first := true
		for _, f := range fields {
			if f.AutoGenerate != schema.AutoNone || f.ReadOnly || f.Hidden {
				continue
			}
			if !first {
				b.WriteString(",\n")
			}
			first = false
			fmt.Fprintf(&b, "  \"%s\": \"<value>\"", f.Name)
		}
		b.WriteString("\n}\n```\n")
		b.WriteString("**Response:** `201` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` with validation errors.\n\n")

		// PUT /{table}/{id}
		fmt.Fprintf(&b, "### PUT %s/{id}\n\n", resourcePath)
		b.WriteString("Update an existing record.\n\n")
		b.WriteString("**Request body:** JSON object with fields to update.\n")
		b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` validation errors, `404` not found.\n\n")

		// PATCH /{table}/{id}
		fmt.Fprintf(&b, "### PATCH %s/{id}\n\n", resourcePath)
		b.WriteString("Sparsely update an existing record. Only fields present in the JSON body are validated and changed.\n\n")
		b.WriteString("**Request body:** JSON object with one or more fields to update.\n")
		b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` validation errors, `404` not found.\n\n")

		// DELETE /{table}/{id}
		fmt.Fprintf(&b, "### DELETE %s/{id}\n\n", resourcePath)
		b.WriteString("Delete a record.\n\n")
		if ent.Config.Scope.SoftDelete {
			b.WriteString("**Note:** This entity uses soft-delete: sets `deleted_at` instead of removing the row.\n\n")
		}
		b.WriteString("**Response:** `204` No Content.\n")
		b.WriteString("**Error:** `404` not found.\n\n")

		// Batch endpoints
		fmt.Fprintf(&b, "### POST %s/_batch\n\n", resourcePath)
		b.WriteString("Batch create (atomic, all-or-nothing).\n\n")
		b.WriteString("```json\n{\n  \"items\": [ { ... }, { ... } ]\n}\n```\n")
		b.WriteString(fmt.Sprintf("Maximum %d items per batch.\n\n", MaxBatchSize))

		fmt.Fprintf(&b, "### PATCH %s/_batch\n\n", resourcePath)
		b.WriteString("Batch update (atomic). Each item must include `id` plus fields to update.\n\n")
		b.WriteString("```json\n{\n  \"items\": [ {\"id\": \"...\", \"...\": \"...\"} ]\n}\n```\n\n")

		fmt.Fprintf(&b, "### DELETE %s/_batch\n\n", resourcePath)
		b.WriteString("Batch delete (atomic).\n\n")
		b.WriteString("```json\n{\n  \"ids\": [ \"id1\", \"id2\" ]\n}\n```\n\n")

		fmt.Fprintf(&b, "**Batch response** (200 committed, 400 rolled back):\n```json\n")
		b.WriteString("{\n")
		b.WriteString("  \"committed\": true,\n")
		b.WriteString("  \"results\": [\n")
		b.WriteString("    { \"index\": 0, \"data\": { ... } },\n")
		b.WriteString("    { \"index\": 1, \"error\": \"validation: ...\", \"fields\": { \"name\": [\"is required\"] } }\n")
		b.WriteString("  ]\n")
		b.WriteString("}\n```\n\n")
	}

	// SSE
	fmt.Fprintf(&b, "### GET %s/_events\n\n", resourcePath)
	b.WriteString("Server-Sent Events stream for real-time entity changes.\n\n")
	b.WriteString("**Event types:** `entity.created`, `entity.updated`, `entity.deleted`\n\n")

	// Custom endpoints
	if len(ent.Config.Endpoints) > 0 {
		b.WriteString("## Custom Endpoints\n\n")
		for _, ep := range ent.Config.Endpoints {
			if ep.Description != "" {
				fmt.Fprintf(&b, "### %s %s\n\n%s\n\n", ep.Method, ep.Path, ep.Description)
			} else {
				fmt.Fprintf(&b, "### %s %s\n\n", ep.Method, ep.Path)
			}
		}
	}

	// Multi-tenant note
	if ent.Config.Scope.MultiTenant {
		b.WriteString("## Multi-tenancy\n\n")
		b.WriteString("All endpoints are scoped by `tenant_id`. The tenant context is derived from the request (middleware-injected).\n\n")
	}

	return b.String()
}

// MountInfo reports how an entity's auto-CRUD routes are mounted. The llm.md
// index asks the app that performed the mounts, not the entity declaration:
// Exposure cannot express that App.View mounts an entity read-only (#358),
// just as it could not express DB-less (#266). The index must describe
// routes that exist.
type MountInfo struct {
	// Mounted reports whether the standard auto-CRUD routes are registered
	// for this entity at all (the app's route predicate: DB attached AND
	// Exposure.CRUD unset-or-true).
	Mounted bool
	// ReadOnly reports a CrudRouteOptions{ReadOnly: true} mount: only the
	// List, Get-by-ID and _events routes exist; write and batch routes
	// were never registered.
	ReadOnly bool
}

// RegistryLLMMD generates a top-level LLM-friendly markdown index that
// lists every registered entity with a link to its detailed llm.md page.
// crudMount reports how the entity's auto-CRUD routes were actually
// registered (the app passes its mount predicate: DB attached AND
// Exposure.CRUD, plus ReadOnly for App.View mounts); nil documents standard
// CRUD for every entity, the pre-#266 behavior. Entities without CRUD
// routes list only their declared custom endpoints, entities with no routes
// at all are omitted, and read-only mounts are counted and labelled as the
// three routes they serve — the index must describe routes that exist. It
// lists every (routed) entity; callers that serve this to a request (the
// /api/llm.md index) MUST filter per request via [registryLLMMD] so the
// index does not disclose entities the caller cannot read.
func RegistryLLMMD(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo) string {
	return registryLLMMD(registry, appName, nil, crudMount)
}

// registryLLMMD renders the index, keeping only entities for which keep
// returns true (a nil keep keeps all). The /api/llm.md handler passes a
// per-request keep that mirrors List's read-scope predicate (owner, tenant,
// RBAC) so an authenticated caller with no grant for an entity gets 403 on
// its rows AND never sees its name, base path or flags in the index.
func registryLLMMD(registry entity.Registry, appName string, keep func(*entity.Entity) bool, crudMount func(*entity.Entity) MountInfo) string {
	var b strings.Builder
	title := appName
	if title == "" {
		title = "API"
	}
	fmt.Fprintf(&b, "# %s: API Reference\n\n", title)
	b.WriteString("Auto-generated LLM-friendly documentation for all registered resources.\n\n")

	entities := registry.AllSorted()
	if len(entities) == 0 {
		b.WriteString("No entities registered.\n")
		return b.String()
	}

	b.WriteString("## Resources\n\n")
	b.WriteString("| Resource | Base Path | Endpoints | Description |\n")
	b.WriteString("|----------|-----------|-----------|-------------|\n")
	for _, ent := range entities {
		if keep != nil && !keep(ent) {
			continue
		}
		// crudMount mirrors route registration: nil (direct callers)
		// documents standard CRUD as before; unmounted lists only declared
		// custom endpoints; an entity with no routes at all is omitted.
		// A read-only mount (App.View) counts and labels the three routes
		// it serves (#358): counting 8 would advertise five that 404/405.
		mount := MountInfo{Mounted: true}
		if crudMount != nil {
			mount = crudMount(ent)
		}
		numEndpoints := len(ent.Config.Endpoints)
		switch {
		case mount.Mounted && mount.ReadOnly:
			numEndpoints += 3 // List, Get-by-ID, _events — the ReadOnly mount
		case mount.Mounted:
			numEndpoints += 8 // standard CRUD + batch + events
		}
		if numEndpoints == 0 {
			continue
		}
		table := ent.GetTable()
		basePath := "/" + table
		llmLink := "/" + table + "/llm.md"
		if ent.Version != "" {
			basePath = ent.Version + basePath
			llmLink = ent.Version + "/" + table + "/llm.md"
		}
		desc := ""
		if mount.ReadOnly {
			desc = "read-only"
		}
		if ent.Config.Scope.SoftDelete {
			if desc != "" {
				desc += ", "
			}
			desc += "soft-delete"
		}
		if ent.Config.Scope.MultiTenant {
			if desc != "" {
				desc += ", "
			}
			desc += "multi-tenant"
		}
		if mount.Mounted {
			// Read-only mounts serve their llm.md too (RegisterCrudRoutes
			// registers the doc route in both modes), so the link stays.
			fmt.Fprintf(&b, "| [%s](%s) | `%s` | %d | %s |\n", ent.GetName(), llmLink, basePath, numEndpoints, desc)
		} else {
			// The per-entity llm.md route rides the CRUD mount; without
			// it the link would 404, so list the name unlinked.
			fmt.Fprintf(&b, "| %s | `%s` | %d | %s |\n", ent.GetName(), basePath, numEndpoints, desc)
		}
	}
	b.WriteString("\n")

	// Link to page documentation if available
	b.WriteString("## Pages\n\n")
	b.WriteString("This site also has UI pages with their own documentation. " +
		"See [/llm-pages.md](/llm-pages.md) for a full index of all screens and pages.\n\n")

	// Quick reference: common patterns
	b.WriteString("## Quick Reference\n\n")
	b.WriteString("### Filtering\n")
	b.WriteString("Append field operators as query parameters: `?status=active&created_at_gt=2024-01-01`\n\n")
	b.WriteString("### Sorting\n")
	b.WriteString("Use `?sort=field` (ascending) or `?sort=-field` (descending).\n\n")
	b.WriteString("### Pagination\n")
	b.WriteString("- **Offset:** `?page=1&limit=20`: returns `{data, total, page, perPage, totalPages}`\n")
	b.WriteString("- **Cursor:** `?cursor=xxx&limit=20`: returns `{data, cursor, hasMore, total}`\n\n")
	b.WriteString("### Includes\n")
	b.WriteString("Eager-load relations: `?include=author,comments`\n")
	b.WriteString("Nested includes: `?include=author.profile`\n")
	b.WriteString("Scoped includes: `?include=comments(status=published)`\n\n")
	b.WriteString("### Batch Operations\n")
	b.WriteString("All batch endpoints are atomic (all-or-nothing). Maximum batch size: ")
	b.WriteString(fmt.Sprintf("%d items.\n", MaxBatchSize))

	return b.String()
}

// LLMMDHandler returns an http.Handler that serves the LLM-friendly markdown
// for a single entity. The schema-disclosure surface is broad (every field,
// validator, relation), so the handler requires an authenticated context:
// the framework's auth chain must have set a user before this fires.
func LLMMDHandler(ent *entity.Entity, opts ...LLMMDOptions) http.Handler {
	md := EntityLLMMD(ent, opts...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := handler.GetUser(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// LLMMDHandlerFor is [LLMMDHandler] with the entity's own access gate
// applied, the form the CRUD routes register.
//
// LLMMDHandler checks only for a session, while List runs the full scope
// chain, so an authenticated caller with no `orders:read` grant got 403 on the
// rows and 200 on the schema: every field name, type and enum of an entity
// they cannot read. The schema is the disclosure, not the row.
//
// It reuses requireScope, so owner, tenant, baseline-session and RBAC all move
// together with the read path instead of being restated here.
func LLMMDHandlerFor(ch *CrudHandler, opts ...LLMMDOptions) http.Handler {
	docs := LLMMDHandler(ch.Entity, opts...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !ch.requireScope(w, r, opRead) {
			return
		}
		docs.ServeHTTP(w, r)
	})
}

// RegistryLLMMDHandler returns an http.Handler that serves the top-level
// LLM-friendly markdown index for all registered entities. Auth-required
// for the same reason as [LLMMDHandler].
//
// The index is rendered per request, filtered to the entities THIS caller
// can list, the same read-scope predicate each entity's List route runs
// (see canListEntity). Serving a construction-time-precomputed document
// instead disclosed every entity's name, base path, endpoint count and
// soft-delete/multi-tenant flags to an authenticated caller who would get
// 403 on the rows. The index is the disclosure, not the row.
func RegistryLLMMDHandler(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		if _, ok := handler.GetUser(ctx); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		md := registryLLMMD(registry, appName, func(ent *entity.Entity) bool {
			return canListEntity(ctx, ent)
		}, crudMount)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// canListEntity reports whether ctx passes every read-scope gate that the
// entity's List route runs, as a boolean (no HTTP response). It reuses the
// SAME in-process predicates List's requireScope delegates to,
// requireOwnerContext, requireTenantContext and CanRead, so the index
// filter cannot drift from the read path. A caller who would receive 401/403
// on an entity's rows is hidden from its index entry too. The baseline
// session gate (requireAuthenticated) is enforced once at the handler entry,
// so it is not re-checked here.
func canListEntity(ctx context.Context, ent *entity.Entity) bool {
	ch := &CrudHandler{Entity: ent}
	if err := ch.requireOwnerContext(ctx); err != nil {
		return false
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return false
	}
	return ch.CanRead(ctx)
}

// fieldTypeLabel returns a human-readable label for a schema field type.
// sanitizeDefault renders a safe summary of a field's default value.
// Complex types (maps, structs, slices) show only the type name.
// Long strings are truncated.
func sanitizeDefault(v any) string {
	switch val := v.(type) {
	case string:
		if len(val) > 50 {
			return val[:50] + "…"
		}
		return val
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, bool:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%T", val)
	}
}

func fieldTypeLabel(t schema.FieldType) string {
	switch t {
	case schema.String:
		return "string"
	case schema.Text:
		return "text"
	case schema.Int:
		return "integer"
	case schema.Float:
		return "float"
	case schema.Decimal:
		return "decimal"
	case schema.Bool:
		return "boolean"
	case schema.Enum:
		return "enum"
	case schema.UUID:
		return "uuid"
	case schema.Timestamp:
		return "timestamp"
	case schema.Date:
		return "date"
	case schema.JSON:
		return "json"
	case schema.Relation:
		return "relation"
	case schema.Image:
		return "image"
	case schema.File:
		return "file"
	default:
		return "string"
	}
}

// relationTypeLabel returns a human-readable label for a relation type.
func relationTypeLabel(t entity.RelationType) string {
	switch t {
	case entity.RelHasOne:
		return "has-one"
	case entity.RelHasMany:
		return "has-many"
	case entity.RelManyToOne:
		return "belongs-to"
	case entity.RelManyToMany:
		return "many-to-many"
	default:
		return "unknown"
	}
}
