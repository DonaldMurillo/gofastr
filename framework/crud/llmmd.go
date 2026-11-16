package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// LLMMDOptions controls what EntityLLMMD documents.
type LLMMDOptions struct {
	// ReadOnly documents only the routes a CrudRouteOptions{ReadOnly: true} mount serves.
	ReadOnly bool
	// Path is the full mount path; empty means "/<table>" behind the entity's Version.
	Path string
	// JSONCase selects the wire keys; a field's WireName takes precedence.
	JSONCase JSONCase
}

// EntityLLMMD renders the markdown page describing ent's CRUD routes,
// fields, filters, includes, events and custom endpoints.
func EntityLLMMD(ent *entity.Entity, opts ...LLMMDOptions) string {
	var opt LLMMDOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	var b strings.Builder
	name := ent.GetName()
	table := ent.GetTable()
	fields := ent.GetFields()
	wireKey := NewCrudHandler(ent, nil).WithJSONCase(opt.JSONCase).WireKey

	fmt.Fprintf(&b, "# %s\n\n", name)
	resourcePath := "/" + table
	switch {
	case opt.Path != "":
		resourcePath = opt.Path
	case ent.Version != "":
		resourcePath = ent.Version + resourcePath
	}
	fmt.Fprintf(&b, "Resource: `%s`\n\n", resourcePath)
	keyCols := ent.KeyColumns()
	wireKeyCols := make([]string, len(keyCols))
	for index, column := range keyCols {
		wireKeyCols[index] = wireKey(column)
	}
	recordPath := resourcePath + KeyPath(keyCols)
	if len(keyCols) > 1 {
		fmt.Fprintf(&b, "Records are keyed on (`%s`): a record's path takes one segment per key column, each value path-escaped, and a batch id joins the escaped values with `/`.\n\n", strings.Join(keyCols, "`, `"))
	}
	if opt.ReadOnly {
		b.WriteString("This resource is served read-only: only the list, get-by-ID and `_events` endpoints are mounted. There are no create, update, delete or batch routes.\n\n")
	}

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
			escaped := make([]string, len(f.Values))
			for i, v := range f.Values {
				escaped[i] = strings.ReplaceAll(v, "|", `\|`)
			}
			notes += "values: " + strings.Join(escaped, `\|`)
		}
		if f.NoQuery {
			if notes != "" {
				notes += ", "
			}
			notes += "not filterable/sortable"
		}
		if opt.ReadOnly {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", wireKey(f.Name), schema.FieldTypeLabel(f.Type, false), notes)
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
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", wireKey(f.Name), schema.FieldTypeLabel(f.Type, false), createCol, updateCol, notes)
	}
	b.WriteString("\n")

	includable := make([]entity.Relation, 0, len(ent.Config.Relations))
	for _, rel := range ent.Config.Relations {
		if !ent.HidesRelation(rel) {
			includable = append(includable, rel)
		}
	}
	if len(includable) > 0 {
		b.WriteString("## Includes\n\n")
		b.WriteString("Use `?include=relationName` to eager-load related data. Separate multiple with commas. Use dots for nested includes.\n\n")
		b.WriteString("| Name | Type | Target | Key |\n")
		b.WriteString("|------|------|--------|-----|\n")
		for _, rel := range includable {
			fmt.Fprintf(&b, "| `%s` | %s | `%s` | `%s` |\n", rel.Name, relationTypeLabel(rel.Type), rel.Entity, strings.Join(rel.ForeignKey, ", "))
		}
		b.WriteString("\n")
		b.WriteString("Examples:\n")
		fmt.Fprintf(&b, "- `?include=%s`\n", includable[0].Name)
		if len(includable) > 1 {
			fmt.Fprintf(&b, "- `?include=%s,%s`\n", includable[0].Name, includable[1].Name)
		}
		for _, rel := range includable {
			if rel.Type == entity.RelHasMany || rel.Type == entity.RelManyToMany {
				fmt.Fprintf(&b, "- `?include=%s(status=published)`: scoped eager-load\n", rel.Name)
				break
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Endpoints\n\n")

	fmt.Fprintf(&b, "### GET %s\n\n", resourcePath)
	b.WriteString("List records with optional filtering, sorting, and pagination.\n\n")
	b.WriteString("**Query parameters:**\n\n")
	b.WriteString("| Parameter | Type | Description |\n")
	b.WriteString("|-----------|------|-------------|\n")
	b.WriteString("| `page` | integer | Page number (offset mode, default 1) |\n")
	defLimit, maxLimit := ListLimits(ent.Config.Pagination.MaxListLimit)
	fmt.Fprintf(&b, "| `limit` | integer | Items per page (default %d, max %d) |\n", defLimit, maxLimit)
	fmt.Fprintf(&b, "| `per_page` | integer | Alias for `limit`; `limit` wins when both are sent (same default %d, max %d) |\n", defLimit, maxLimit)
	fmt.Fprintf(&b, "| `offset` | integer | Rows to skip, replacing the page-derived skip (offset mode only; refused past %d rows) |\n", maxLimit*1000)
	b.WriteString("| `sort` | string | Sort field (prefix with `-` for descending, e.g. `-created_at`) |\n")
	b.WriteString("| `cursor` | string | Opaque cursor for keyset pagination. Presence switches to cursor mode. |\n")
	b.WriteString("| `direction` | string | Cursor walk direction: `forward` (default) or `backward` |\n")
	b.WriteString("| `include` | string | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")

	visibleFields := make([]schema.Field, 0, len(fields))
	for _, f := range fields {
		if !f.Hidden && !f.NoQuery {
			visibleFields = append(visibleFields, f)
		}
	}
	if len(visibleFields) > 0 {
		b.WriteString("**Filter operators** (append one to a filterable field; the parser refuses it on a type it does not suit, with a 400):\n\n")
		b.WriteString("| Suffix | Operator | Applies to | Example |\n")
		b.WriteString("|--------|----------|------------|----------|\n")
		spell := filter.SpellerFor(fields)
		writeLLMOpRow(&b, "", filter.OpEq, "equals", "active", visibleFields, spell)
		for _, s := range filter.FilterSuffixes {
			label, sample := llmOpLabelSample(s.Op)
			writeLLMOpRow(&b, s.Suffix, s.Op, label, sample, visibleFields, spell)
		}
		b.WriteString("\n")
		b.WriteString("Or name the operator by word in brackets after the whole field name: `?<field>[op]=value`, op one of `eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `like`, `in`.\n")
		writeLLMAmbiguousKeys(&b, fields, spell)
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
	b.WriteString("  \"hasMore\": true\n")
	b.WriteString("}\n```\n\n")

	fmt.Fprintf(&b, "### GET %s\n\n", recordPath)
	b.WriteString("Retrieve a single record by ID.\n\n")
	b.WriteString("| Parameter | Location | Description |\n")
	b.WriteString("|-----------|----------|-------------|\n")
	if len(keyCols) > 1 {
		for i, c := range keyCols {
			fmt.Fprintf(&b, "| `%s` | path | Primary-key column %d of %d |\n", c, i+1, len(keyCols))
		}
	} else {
		b.WriteString("| `id` | path | Record primary key |\n")
	}
	b.WriteString("| `include` | query | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")
	b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
	b.WriteString("**Error:** `404` if not found.\n\n")

	if !opt.ReadOnly {

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
			key, _ := json.Marshal(wireKey(f.Name))
			fmt.Fprintf(&b, "  %s: \"<value>\"", key)
		}
		b.WriteString("\n}\n```\n")
		b.WriteString("**Response:** `201` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` with validation errors.\n\n")

		fmt.Fprintf(&b, "### PUT %s\n\n", recordPath)
		b.WriteString("Update an existing record.\n\n")
		b.WriteString("**Request body:** JSON object with fields to update.\n")
		b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` validation errors, `404` not found.\n\n")

		fmt.Fprintf(&b, "### PATCH %s\n\n", recordPath)
		b.WriteString("Sparsely update an existing record. Only fields present in the JSON body are validated and changed.\n\n")
		b.WriteString("**Request body:** JSON object with one or more fields to update.\n")
		b.WriteString("**Response:** `200` with `{\"data\": { ... }}`.\n")
		b.WriteString("**Error:** `400` validation errors, `404` not found.\n\n")

		fmt.Fprintf(&b, "### DELETE %s\n\n", recordPath)
		b.WriteString("Delete a record.\n\n")
		if ent.Config.Scope.SoftDelete {
			fmt.Fprintf(&b, "**Note:** This entity uses soft-delete: sets `%s` instead of removing the row.\n\n", wireKey("deleted_at"))
		}
		b.WriteString("**Response:** `204` No Content.\n")
		b.WriteString("**Error:** `404` not found.\n\n")

		fmt.Fprintf(&b, "### POST %s/_batch\n\n", resourcePath)
		b.WriteString("Batch create (atomic, all-or-nothing).\n\n")
		b.WriteString("```json\n{\n  \"items\": [ { ... }, { ... } ]\n}\n```\n")
		b.WriteString(fmt.Sprintf("Maximum %d items per batch.\n\n", MaxBatchSize))

		fmt.Fprintf(&b, "### PATCH %s/_batch\n\n", resourcePath)
		fmt.Fprintf(&b, "Batch update (atomic). Each item must include `%s` plus fields to update.\n\n", strings.Join(wireKeyCols, "`, `"))
		itemKeys := make([]string, len(keyCols))
		for i, c := range keyCols {
			itemKeys[i] = fmt.Sprintf("%q: \"...\"", wireKey(c))
		}
		fmt.Fprintf(&b, "```json\n{\n  \"items\": [ {%s, \"...\": \"...\"} ]\n}\n```\n\n", strings.Join(itemKeys, ", "))

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

		if st := ent.Config.States; st != nil && len(RoutableTransitions(st)) > 0 {
			b.WriteString("## States\n\n")
			fmt.Fprintf(&b, "`%s` starts at one of: %s.\n\n", wireKey(st.Field), quoteValues(st.InitialValues(fields)))
			b.WriteString("| Move | Route | From → To | Stamp |\n")
			b.WriteString("|------|-------|-----------|-------|\n")
			for _, t := range RoutableTransitions(st) {
				move := "`" + t.Key + "`"
				if t.Label != "" {
					move += " (" + t.Label + ")"
				}
				stamp := "—"
				if t.Stamp != "" {
					stamp = "`" + wireKey(t.Stamp) + "`"
				}
				fmt.Fprintf(&b, "| %s | `POST %s/transitions/%s` | `%s` → `%s` | %s |\n",
					move, recordPath, t.Key, strings.Join(t.From, "`, `"), t.To, stamp)
			}
			b.WriteString("\n")
			if !st.Advisory {
				fmt.Fprintf(&b, "`%s` and every stamp change only through these moves: a create may start at an initial value, and an update that sends the stored value back is not a change (any other write answers 422).\n\n", wireKey(st.Field))
			}
		}
	}

	fmt.Fprintf(&b, "### GET %s/_events\n\n", resourcePath)
	b.WriteString("Server-Sent Events stream for real-time entity changes.\n\n")
	b.WriteString("**Event types:** `entity.created`, `entity.updated`, `entity.deleted`\n\n")

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

	if ent.Config.Scope.MultiTenant {
		b.WriteString("## Multi-tenancy\n\n")
		fmt.Fprintf(&b, "All endpoints are scoped by `%s`. The tenant context is derived from the request (middleware-injected).\n\n", wireKey(ent.Config.TenantColumn()))
	}

	return b.String()
}

// MountInfo reports how the app mounted an entity's auto-CRUD routes.
type MountInfo struct {
	// Mounted reports whether the auto-CRUD routes are registered at all.
	Mounted bool
	// ReadOnly reports a CrudRouteOptions{ReadOnly: true} mount.
	ReadOnly bool
	// Path is the full mount path; empty derives it from the table and Version.
	Path string
}

// RegistryLLMMD renders the markdown index of every registered entity
// with routes, unfiltered: a handler serving it filters per caller through
// registryLLMMD. crudMount reports each entity's mount, nil meaning
// standard CRUD; an empty pagesIndex omits the Pages section.
func RegistryLLMMD(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo, pagesIndex string) string {
	return registryLLMMD(registry, appName, nil, crudMount, pagesIndex)
}

// registryLLMMD renders the index of the entities keep accepts (nil keeps all).
func registryLLMMD(registry entity.Registry, appName string, keep func(*entity.Entity) bool, crudMount func(*entity.Entity) MountInfo, pagesIndex string) string {
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
		mount := MountInfo{Mounted: true}
		if crudMount != nil {
			mount = crudMount(ent)
		}
		numEndpoints := len(ent.Config.Endpoints)
		switch {
		case mount.Mounted && mount.ReadOnly:
			numEndpoints += 3
		case mount.Mounted:
			numEndpoints += 8
		}
		if numEndpoints == 0 {
			continue
		}
		table := ent.GetTable()
		basePath := "/" + table
		llmLink := "/" + table + "/llm.md"
		switch {
		case mount.Path != "":
			basePath = mount.Path
			llmLink = mount.Path + "/llm.md"
		case ent.Version != "":
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
			fmt.Fprintf(&b, "| [%s](%s) | `%s` | %d | %s |\n", ent.GetName(), llmLink, basePath, numEndpoints, desc)
		} else {
			fmt.Fprintf(&b, "| %s | `%s` | %d | %s |\n", ent.GetName(), basePath, numEndpoints, desc)
		}
	}
	b.WriteString("\n")

	if pagesIndex != "" {
		b.WriteString("## Pages\n\n")
		fmt.Fprintf(&b, "This site also has UI pages with their own documentation. "+
			"See [%s](%s) for a full index of all screens and pages.\n\n", pagesIndex, pagesIndex)
	}

	b.WriteString("## Quick Reference\n\n")
	b.WriteString("### Filtering\n")
	b.WriteString("Append field operators as query parameters: `?status=active&created_at_gt=2024-01-01`\n\n")
	b.WriteString("### Sorting\n")
	b.WriteString("Use `?sort=field` (ascending) or `?sort=-field` (descending).\n\n")
	b.WriteString("### Pagination\n")
	b.WriteString("- **Offset:** `?page=1&limit=20`: returns `{data, total, page, perPage, totalPages}`\n")
	b.WriteString("- **Cursor:** `?cursor=xxx&limit=20`: returns `{data, cursor, hasMore}`\n\n")
	b.WriteString("### Includes\n")
	b.WriteString("Eager-load relations: `?include=author,comments`\n")
	b.WriteString("Nested includes: `?include=author.profile`\n")
	b.WriteString("Scoped includes: `?include=comments(status=published)`\n\n")
	b.WriteString("### Batch Operations\n")
	b.WriteString("All batch endpoints are atomic (all-or-nothing). Maximum batch size: ")
	b.WriteString(fmt.Sprintf("%d items.\n", MaxBatchSize))

	return b.String()
}

// LLMMDHandler serves ent's llm.md page to an authenticated caller; any
// other gets 401.
func LLMMDHandler(ent *entity.Entity, opts ...LLMMDOptions) http.Handler {
	md := EntityLLMMD(ent, opts...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := handler.GetUser(r.Context()); !ok {
			i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// LLMMDHandlerFor is [LLMMDHandler] behind the read gate of ch's List
// route, the form the CRUD routes register.
func LLMMDHandlerFor(ch *CrudHandler, opts ...LLMMDOptions) http.Handler {
	var options LLMMDOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	options.JSONCase = ch.JSONCase
	docs := LLMMDHandler(ch.Entity, options)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !ch.requireScope(w, r, opRead) {
			return
		}
		docs.ServeHTTP(w, r)
	})
}

// RegistryLLMMDHandler serves the llm.md index to an authenticated caller,
// listing only the entities that caller can list.
func RegistryLLMMDHandler(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo, pagesIndex string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		if _, ok := handler.GetUser(ctx); !ok {
			i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
			return
		}
		md := registryLLMMD(registry, appName, func(ent *entity.Entity) bool {
			return canListEntity(ctx, ent)
		}, crudMount, pagesIndex)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// canListEntity reports whether ctx passes the read gates of ent's List
// route other than the session check.
func canListEntity(ctx context.Context, ent *entity.Entity) bool {
	ch := &CrudHandler{Entity: ent}
	if err := ch.requireOwnerContext(ctx); err != nil {
		return false
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return false
	}
	return access.ScopeAllows(ctx, access.Permission(ent.GetTable()+":read")) && ch.CanRead(ctx)
}

// sanitizeDefault renders a default value: a scalar as itself, a string cut
// to 50 runes, anything else as its type name.
func sanitizeDefault(v any) string {
	switch val := v.(type) {
	case string:
		if runes := []rune(val); len(runes) > 50 {
			return string(runes[:50]) + "…"
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

func quoteValues(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	return strings.Join(quoted, ", ")
}

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

// llmOpLabelSample is an operator's label and its text sample value.
func llmOpLabelSample(op filter.FilterOp) (label, sample string) {
	switch op {
	case filter.OpNe:
		return "not equal", "active"
	case filter.OpGt:
		return "greater than", "100"
	case filter.OpGte:
		return "greater than or equal", "100"
	case filter.OpLt:
		return "less than", "100"
	case filter.OpLte:
		return "less than or equal", "100"
	case filter.OpLike:
		return "LIKE (contains)", "search"
	case filter.OpIn:
		return "IN (comma-separated)", "a,b,c"
	}
	return "equals", "active"
}

// llmFilterSample is a sample value of type t that filter.CoerceValue accepts.
func llmFilterSample(t schema.FieldType, op filter.FilterOp, text string) string {
	var one, many string
	switch t {
	case schema.Int, schema.Float, schema.Decimal, schema.Relation:
		one, many = "100", "1,2,3"
	case schema.Bool:
		one, many = "true", "true,false"
	case schema.Timestamp:
		one, many = "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z,2026-02-01T00:00:00Z"
	case schema.Date:
		one, many = "2026-01-01", "2026-01-01,2026-02-01"
	default:
		return text
	}
	if op == filter.OpIn {
		return many
	}
	return one
}

var llmAllFieldTypes = []schema.FieldType{
	schema.String, schema.Text, schema.Int, schema.Float, schema.Decimal,
	schema.Bool, schema.Enum, schema.UUID, schema.Timestamp, schema.Date,
	schema.JSON, schema.Relation, schema.Image, schema.File,
}

// writeLLMOpRow writes op's filter-table row, its example naming the first
// visible field op suits.
func writeLLMOpRow(b *strings.Builder, suffix string, op filter.FilterOp, label, sample string, visible []schema.Field, spell filter.KeySpeller) {
	var applies []string
	for _, t := range llmAllFieldTypes {
		if filter.OpSuitsType(op, t) {
			applies = append(applies, schema.FieldTypeLabel(t, false))
		}
	}
	key := "<field>" + suffix
	for _, f := range visible {
		if filter.OpSuitsType(op, f.Type) {
			key = spell.Key(f.Name, op)
			sample = llmFilterSample(f.Type, op, sample)
			break
		}
	}
	fmt.Fprintf(b, "| `%s` | %s | %s | `%s=%s` |\n",
		suffix, label, strings.Join(applies, ", "), key, sample)
}

// writeLLMAmbiguousKeys gives the bracket spellings of each bare key that
// reads two ways.
func writeLLMAmbiguousKeys(b *strings.Builder, fields []schema.Field, spell filter.KeySpeller) {
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		names := []string{f.Name}
		if f.WireName != "" && f.WireName != f.Name {
			names = append(names, f.WireName)
		}
		for _, name := range names {
			if !filter.IsAmbiguousKey(name, spell.Named) {
				continue
			}
			for _, s := range filter.FilterSuffixes {
				if base, ok := strings.CutSuffix(name, s.Suffix); ok {
					fmt.Fprintf(b, "`?%s=` reads two ways and answers 400: send `?%s=` (the `%s` field) or `?%s=` (`%s` on `%s`).\n",
						name, filter.BracketKey(name, filter.OpEq), name, filter.BracketKey(base, s.Op), s.Op, base)
					break
				}
			}
		}
	}
}
