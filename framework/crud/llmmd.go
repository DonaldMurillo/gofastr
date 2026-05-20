package crud

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EntityLLMMD generates an LLM-friendly markdown document describing all
// CRUD endpoints for a single entity. The output is designed to be
// immediately useful as context for an LLM agent — concise, structured,
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
func EntityLLMMD(ent *entity.Entity) string {
	var b strings.Builder
	name := ent.GetName()
	table := ent.GetTable()
	fields := ent.GetFields()

	fmt.Fprintf(&b, "# %s\n\n", name)
	fmt.Fprintf(&b, "Resource: `/%s`\n\n", table)

	// --- Field reference ---
	b.WriteString("## Fields\n\n")
	b.WriteString("| Field | Type | Create | Update | Notes |\n")
	b.WriteString("|-------|------|--------|--------|-------|\n")
	for _, f := range fields {
		if f.Hidden {
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
				fmt.Fprintf(&b, "- `?include=%s(status=published)` — scoped eager-load\n", rel.Name)
				break
			}
		}
		b.WriteString("\n")
	}

	// --- Endpoints ---
	b.WriteString("## Endpoints\n\n")

	// GET /{table} — List
	fmt.Fprintf(&b, "### GET /%s\n\n", table)
	b.WriteString("List records with optional filtering, sorting, and pagination.\n\n")
	b.WriteString("**Query parameters:**\n\n")
	b.WriteString("| Parameter | Type | Description |\n")
	b.WriteString("|-----------|------|-------------|\n")
	b.WriteString("| `page` | integer | Page number (offset mode, default 1) |\n")
	b.WriteString("| `limit` | integer | Items per page (default 20, max 100) |\n")
	b.WriteString("| `sort` | string | Sort field (prefix with `-` for descending, e.g. `-created_at`) |\n")
	b.WriteString("| `cursor` | string | Opaque cursor for keyset pagination. Presence switches to cursor mode. |\n")
	b.WriteString("| `direction` | string | Cursor walk direction: `forward` (default) or `backward` |\n")
	b.WriteString("| `include` | string | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")

	// Filter operators
	visibleFields := make([]schema.Field, 0, len(fields))
	for _, f := range fields {
		if !f.Hidden {
			visibleFields = append(visibleFields, f)
		}
	}
	if len(visibleFields) > 0 {
		b.WriteString("**Filter operators** (append to any visible field name):\n\n")
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
	fmt.Fprintf(&b, "### GET /%s/{id}\n\n", table)
	b.WriteString("Retrieve a single record by ID.\n\n")
	b.WriteString("| Parameter | Location | Description |\n")
	b.WriteString("|-----------|----------|-------------|\n")
	b.WriteString("| `id` | path | Record primary key |\n")
	b.WriteString("| `include` | query | Comma-separated relations to eager-load |\n")
	b.WriteString("\n")
	b.WriteString("**Response:** `200` with the entity object.\n")
	b.WriteString("**Error:** `404` if not found.\n\n")

	// POST /{table}
	fmt.Fprintf(&b, "### POST /%s\n\n", table)
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
	b.WriteString("**Response:** `201` with the created entity object.\n")
	b.WriteString("**Error:** `400` with validation errors.\n\n")

	// PUT /{table}/{id}
	fmt.Fprintf(&b, "### PUT /%s/{id}\n\n", table)
	b.WriteString("Update an existing record.\n\n")
	b.WriteString("**Request body:** JSON object with fields to update.\n")
	b.WriteString("**Response:** `200` with the updated entity object.\n")
	b.WriteString("**Error:** `400` validation errors, `404` not found.\n\n")

	// DELETE /{table}/{id}
	fmt.Fprintf(&b, "### DELETE /%s/{id}\n\n", table)
	b.WriteString("Delete a record.\n\n")
	if ent.Config.SoftDelete {
		b.WriteString("**Note:** This entity uses soft-delete — sets `deleted_at` instead of removing the row.\n\n")
	}
	b.WriteString("**Response:** `204` No Content.\n")
	b.WriteString("**Error:** `404` not found.\n\n")

	// Batch endpoints
	fmt.Fprintf(&b, "### POST /%s/_batch\n\n", table)
	b.WriteString("Batch create (atomic — all-or-nothing).\n\n")
	b.WriteString("```json\n{\n  \"items\": [ { ... }, { ... } ]\n}\n```\n")
	b.WriteString(fmt.Sprintf("Maximum %d items per batch.\n\n", MaxBatchSize))

	fmt.Fprintf(&b, "### PATCH /%s/_batch\n\n", table)
	b.WriteString("Batch update (atomic). Each item must include `id` plus fields to update.\n\n")
	b.WriteString("```json\n{\n  \"items\": [ {\"id\": \"...\", \"...\": \"...\"} ]\n}\n```\n\n")

	fmt.Fprintf(&b, "### DELETE /%s/_batch\n\n", table)
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

	// SSE
	fmt.Fprintf(&b, "### GET /%s/_events\n\n", table)
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
	if ent.Config.MultiTenant {
		b.WriteString("## Multi-tenancy\n\n")
		b.WriteString("All endpoints are scoped by `tenant_id`. The tenant context is derived from the request (middleware-injected).\n\n")
	}

	return b.String()
}

// RegistryLLMMD generates a top-level LLM-friendly markdown index that
// lists every registered entity with a link to its detailed llm.md page.
func RegistryLLMMD(registry entity.Registry, appName string) string {
	var b strings.Builder
	title := appName
	if title == "" {
		title = "API"
	}
	fmt.Fprintf(&b, "# %s — API Reference\n\n", title)
	b.WriteString("Auto-generated LLM-friendly documentation for all registered resources.\n\n")

	entities := registry.All()
	if len(entities) == 0 {
		b.WriteString("No entities registered.\n")
		return b.String()
	}

	b.WriteString("## Resources\n\n")
	b.WriteString("| Resource | Base Path | Endpoints | Description |\n")
	b.WriteString("|----------|-----------|-----------|-------------|\n")
	for _, ent := range entities {
		table := ent.GetTable()
		numEndpoints := 8 // standard CRUD + batch + events
		numEndpoints += len(ent.Config.Endpoints)
		desc := ""
		if ent.Config.SoftDelete {
			desc = "soft-delete"
		}
		if ent.Config.MultiTenant {
			if desc != "" {
				desc += ", "
			}
			desc += "multi-tenant"
		}
		fmt.Fprintf(&b, "| [%s](/%s/llm.md) | `/%s` | %d | %s |\n", ent.GetName(), table, table, numEndpoints, desc)
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
	b.WriteString("- **Offset:** `?page=1&limit=20` — returns `{data, total, page, perPage, totalPages}`\n")
	b.WriteString("- **Cursor:** `?cursor=xxx&limit=20` — returns `{data, cursor, hasMore, total}`\n\n")
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
// for a single entity. Content-Type is text/markdown.
func LLMMDHandler(ent *entity.Entity) http.Handler {
	md := EntityLLMMD(ent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(md))); w.Write([]byte(md))
	})
}

// RegistryLLMMDHandler returns an http.Handler that serves the top-level
// LLM-friendly markdown index for all registered entities.
func RegistryLLMMDHandler(registry entity.Registry, appName string) http.Handler {
	md := RegistryLLMMD(registry, appName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(md))); w.Write([]byte(md))
	})
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
