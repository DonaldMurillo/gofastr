package framework

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/filter"
)

// IncludeNode represents one segment of a (possibly nested) ?include=
// expression. The tree is rooted at the request's entity; each node carries
// the relation taken to reach it and any deeper child includes.
//
// Filters narrows the eager-load to rows on the target that match every
// scoped predicate, e.g. include=comments(status=published) attaches only
// published comments. Suffixes (_gt/_gte/_lt/_lte/_like/_in) work the same
// way they do for top-level filters.
type IncludeNode struct {
	Name     string                // segment name (matches the relation's Name)
	Relation entity.Relation       // relation declared on the parent entity
	Target   *entity.Entity        // the entity Reached by following Relation
	Filters  []filter.ParsedFilter // scoped filters applied during eager-load
	Children []*IncludeNode        // deeper includes, e.g. for "author.profile" the "profile" child of "author"
	childMap map[string]*IncludeNode
}

// parseIncludeTree splits comma-separated dotted include paths and resolves
// each segment against the registry. Returns the roots of the include forest.
//
// Example: "author.profile, comments" against a posts entity yields two
// roots: author (with profile as a child) and comments (no children).
func parseIncludeTree(r *http.Request, ent *entity.Entity, registry *Registry) ([]*IncludeNode, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("include"))
	if raw == "" {
		return nil, nil
	}
	if registry == nil {
		// Fall back gracefully: dotted paths require the registry, but flat
		// paths can still be resolved against the request's entity directly.
		return parseIncludesFlat(raw, ent)
	}

	var roots []*IncludeNode
	rootMap := map[string]*IncludeNode{}

	for _, path := range splitIncludeList(raw) {
		segments := splitIncludePath(path)
		if len(segments) == 0 {
			continue
		}

		siblings := &roots
		siblingMap := rootMap
		currentEntity := ent

		for i, segRaw := range segments {
			seg, filterClause := splitSegmentFilter(segRaw)
			rel, ok := relationByName(currentEntity, seg)
			if !ok {
				return nil, fmt.Errorf("unknown include %q (segment %q has no relation on entity %q)", path, seg, currentEntity.GetName())
			}
			// Resolve the target entity. We only HARD-REQUIRE registration when
			// there are deeper segments to walk (otherwise this segment is a
			// leaf and EagerLoad just hits the relation's target table by
			// name).
			target, err := registry.Get(rel.Entity)
			if err != nil {
				if i < len(segments)-1 {
					return nil, fmt.Errorf("include %q: target entity %q not registered (required for nested includes)", path, rel.Entity)
				}
				target = nil
			}
			node, exists := siblingMap[seg]
			if !exists {
				node = &IncludeNode{
					Name:     seg,
					Relation: rel,
					Target:   target,
					childMap: map[string]*IncludeNode{},
				}
				siblingMap[seg] = node
				*siblings = append(*siblings, node)
			}
			if filterClause != "" {
				// Parse scoped filters against the TARGET entity's fields when
				// known. With no target, we accept the field name as-is
				// (unsafe to validate without a schema; the SQL will fail at
				// query time if the column doesn't exist).
				var targetFields []schema.Field
				if target != nil {
					targetFields = target.GetFields()
				}
				parsed, err := parseScopedFilters(filterClause, targetFields, path)
				if err != nil {
					return nil, err
				}
				node.Filters = append(node.Filters, parsed...)
			}
			siblings = &node.Children
			siblingMap = node.childMap
			if target != nil {
				currentEntity = target
			}
		}
	}
	return roots, nil
}

// splitSegmentFilter splits "rel(filter=val)" into ("rel", "filter=val").
// Returns the unparenthesized name with empty filter if no parens are
// present. Treats unbalanced parens as a parse error by returning the raw
// segment with an empty filter — the relation lookup will then fail with
// a clear error.
func splitSegmentFilter(seg string) (name, filter string) {
	open := strings.Index(seg, "(")
	if open < 0 {
		return seg, ""
	}
	close := strings.LastIndex(seg, ")")
	if close < open {
		return seg, ""
	}
	return seg[:open], seg[open+1 : close]
}

// splitIncludeList splits the top-level comma-separated include list while
// respecting parentheses — "comments(status=draft,body_like=x),author"
// must split into ["comments(status=draft,body_like=x)", "author"] not
// into four broken fragments.
func splitIncludeList(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if part := strings.TrimSpace(s[start:i]); part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

// splitIncludePath splits a single include path on dots, but only at
// depth 0 so filter clauses keep their parenthesised content intact.
func splitIncludePath(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '.':
			if depth == 0 {
				if part := strings.TrimSpace(s[start:i]); part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

// parseScopedFilters parses "status=published,body_like=%foo%" into a slice
// of ParsedFilter, honouring the same suffix operators (_gt/_gte/_lt/_lte/
// _like/_in) that top-level filters do. fields can be nil — in that case
// every field name is accepted at parse time.
func parseScopedFilters(raw string, fields []schema.Field, pathForErrors string) ([]filter.ParsedFilter, error) {
	knownField := map[string]bool{}
	for _, f := range fields {
		knownField[f.Name] = true
	}
	suffixes := []struct {
		suffix string
		op     filter.FilterOp
	}{
		{"_gte", filter.OpGte}, {"_lte", filter.OpLte},
		{"_gt", filter.OpGt}, {"_lt", filter.OpLt},
		{"_like", filter.OpLike}, {"_in", filter.OpIn},
	}
	var out []filter.ParsedFilter
	for _, kv := range strings.Split(raw, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return nil, fmt.Errorf("include %q: scoped filter %q missing =", pathForErrors, kv)
		}
		key, value := kv[:eq], kv[eq+1:]
		field := key
		op := filter.OpEq
		for _, s := range suffixes {
			if strings.HasSuffix(key, s.suffix) {
				field = strings.TrimSuffix(key, s.suffix)
				op = s.op
				break
			}
		}
		if fields != nil && !knownField[field] {
			return nil, fmt.Errorf("include %q: scoped field %q not on target entity", pathForErrors, field)
		}
		if op == filter.OpIn {
			for _, v := range strings.Split(value, "|") {
				out = append(out, filter.ParsedFilter{Field: field, Op: filter.OpIn, Value: v})
			}
		} else {
			out = append(out, filter.ParsedFilter{Field: field, Op: op, Value: value})
		}
	}
	return out, nil
}

// parseIncludesFlat is the no-registry fallback: only top-level relation
// names are supported (no dots). Dotted paths produce an error.
func parseIncludesFlat(raw string, ent *entity.Entity) ([]*IncludeNode, error) {
	var out []*IncludeNode
	for _, p := range splitNonEmpty(raw, ",") {
		if strings.Contains(p, ".") {
			return nil, fmt.Errorf("nested include %q requires a registry", p)
		}
		rel, ok := relationByName(ent, p)
		if !ok {
			return nil, fmt.Errorf("unknown include %q", p)
		}
		out = append(out, &IncludeNode{Name: p, Relation: rel})
	}
	return out, nil
}

// splitNonEmpty splits and drops empty fragments after trimming.
func splitNonEmpty(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// relationByName looks up a Relation on an entity by name.
func relationByName(ent *entity.Entity, name string) (entity.Relation, bool) {
	for _, rel := range ent.Config.Relations {
		if rel.Name == name {
			return rel, true
		}
	}
	return entity.Relation{}, false
}

// applyIncludeTree eager-loads the include forest onto the parent rows. Top-
// level rows are JSON-cased (as they came out of scanRows); nested rows are
// kept in raw DB casing during recursion so further EagerLoad calls can find
// foreign-key columns. The very last step deep-converts everything attached
// under the include keys to JSON case.
func (ch *CrudHandler) applyIncludeTree(ctx context.Context, rows []map[string]any, nodes []*IncludeNode) error {
	if len(rows) == 0 || len(nodes) == 0 {
		return nil
	}
	pkKey := ch.convertKey(ch.PrimaryKey)
	ids := collectStringIDs(rows, pkKey)

	// Build the result map shape once; loadIncludeNode populates it relation
	// by relation so scoped filters can be applied per-node.
	loaded := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		loaded[id] = make(map[string]any)
	}
	for _, node := range nodes {
		if err := loadIncludeNode(ctx, ch.DB, ch.Entity.GetTable(), ch.PrimaryKey, node, ids, loaded); err != nil {
			return fmt.Errorf("eager load %s: %w", node.Relation.Name, err)
		}
	}

	// Recurse into each node that has children.
	for _, node := range nodes {
		if len(node.Children) == 0 || node.Target == nil {
			continue
		}
		nestedRows := gatherLoadedRows(loaded, node.Relation.Name)
		if len(nestedRows) == 0 {
			continue
		}
		if err := ch.recurseLoadOnRawRows(ctx, node.Target, node.Children, nestedRows); err != nil {
			return err
		}
	}

	// Attach to parent rows + deep-convert keys (top-level outer key uses
	// JSON case, the entire nested subtree gets the same treatment).
	for _, row := range rows {
		idVal, ok := row[pkKey]
		if !ok || idVal == nil {
			continue
		}
		id := fmt.Sprintf("%v", idVal)
		bucket := loaded[id]
		for _, node := range nodes {
			outKey := ch.convertKey(node.Relation.Name)
			val, present := bucket[node.Relation.Name]
			row[outKey] = ch.formatRelationValueDeep(node.Relation, val, present)
		}
	}
	return nil
}

// recurseLoadOnRawRows operates on rows that are still in raw DB casing — the
// nested data EagerLoad produced. It re-runs EagerLoad with each child's
// target relation against those rows, then recurses again.
func (ch *CrudHandler) recurseLoadOnRawRows(ctx context.Context, target *entity.Entity, children []*IncludeNode, rawRows []map[string]any) error {
	pk := target.PrimaryKey
	if pk == "" {
		pk = "id"
	}
	ids := collectStringIDs(rawRows, pk)
	if len(ids) == 0 {
		return nil
	}
	loaded := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		loaded[id] = make(map[string]any)
	}
	for _, node := range children {
		if err := loadIncludeNode(ctx, ch.DB, target.GetTable(), pk, node, ids, loaded); err != nil {
			return fmt.Errorf("eager load %s: %w", node.Relation.Name, err)
		}
	}
	// Further recursion for grandchildren.
	for _, node := range children {
		if len(node.Children) == 0 || node.Target == nil {
			continue
		}
		nestedRows := gatherLoadedRows(loaded, node.Relation.Name)
		if len(nestedRows) == 0 {
			continue
		}
		if err := ch.recurseLoadOnRawRows(ctx, node.Target, node.Children, nestedRows); err != nil {
			return err
		}
	}
	// Attach onto the raw rows under the raw relation name (no case conversion
	// here — that happens once at the outermost merge).
	for _, row := range rawRows {
		idVal, ok := row[pk]
		if !ok || idVal == nil {
			continue
		}
		id := fmt.Sprintf("%v", idVal)
		bucket := loaded[id]
		for _, node := range children {
			val, present := bucket[node.Relation.Name]
			row[node.Relation.Name] = rawRelationValue(node.Relation, val, present)
		}
	}
	return nil
}

// gatherLoadedRows walks loaded[parentID][relName] entries and returns the
// flat list of nested rows, regardless of HasOne/HasMany/etc. shape.
func gatherLoadedRows(loaded map[string]map[string]any, relName string) []map[string]any {
	var out []map[string]any
	for _, bucket := range loaded {
		v, ok := bucket[relName]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			out = append(out, x)
		case []map[string]any:
			out = append(out, x...)
		}
	}
	return out
}

// collectStringIDs reads pkKey from each row and returns the values as
// strings. Skips rows without a usable id.
func collectStringIDs(rows []map[string]any, pkKey string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if v, ok := row[pkKey]; ok && v != nil {
			out = append(out, fmt.Sprintf("%v", v))
		}
	}
	return out
}

// rawRelationValue normalises a relation-attached value while keeping raw DB
// keys (no JSON casing). Used during recursive nested loading.
func rawRelationValue(rel entity.Relation, val any, present bool) any {
	switch rel.Type {
	case entity.RelHasMany, entity.RelManyToMany:
		if !present {
			return []map[string]any{}
		}
		slice, ok := val.([]map[string]any)
		if !ok {
			return []map[string]any{}
		}
		return slice
	default:
		if !present {
			return nil
		}
		m, ok := val.(map[string]any)
		if !ok {
			return nil
		}
		return m
	}
}

// formatRelationValueDeep is like formatRelationValue but recursively
// converts every nested map's keys to JSON case, including any subtrees
// previously attached during recurseLoadOnRawRows.
func (ch *CrudHandler) formatRelationValueDeep(rel entity.Relation, val any, present bool) any {
	switch rel.Type {
	case entity.RelHasMany, entity.RelManyToMany:
		if !present {
			return []map[string]any{}
		}
		slice, ok := val.([]map[string]any)
		if !ok {
			return []map[string]any{}
		}
		out := make([]map[string]any, len(slice))
		for i, m := range slice {
			out[i] = ch.deepConvertMap(m).(map[string]any)
		}
		return out
	default:
		if !present {
			return nil
		}
		m, ok := val.(map[string]any)
		if !ok {
			return nil
		}
		return ch.deepConvertMap(m).(map[string]any)
	}
}

// deepConvertMap walks a value tree, applying ch.convertKey to every map key
// (including keys inside nested maps and slices). Non-map values pass through
// unchanged.
func (ch *CrudHandler) deepConvertMap(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[ch.convertKey(k)] = ch.deepConvertMap(val)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(x))
		for i, m := range x {
			out[i] = ch.deepConvertMap(m).(map[string]any)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = ch.deepConvertMap(v)
		}
		return out
	default:
		return v
	}
}
