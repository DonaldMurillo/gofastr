package crud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

const (
	maxIncludeDepth = 4

	maxIncludePaths = 32

	maxIncludeRows = 20000
)

var errIncludeBudget = i18n.NewError(reasonIncludeBudget)

type includeBudget struct{ remaining int }

func newIncludeBudget() *includeBudget { return &includeBudget{remaining: maxIncludeRows} }

func (b *includeBudget) spend(n int) error {
	if b == nil {
		return nil
	}
	b.remaining -= n
	if b.remaining < 0 {
		return errIncludeBudget
	}
	return nil
}

// convertedSubtree is a converted map and the row references it serialises to.
type convertedSubtree struct {
	out   map[string]any
	nodes int
}

// IncludeNode represents one segment of a (possibly nested) ?include=
// expression. The tree is rooted at the request's entity; each node carries
// the relation taken to reach it and any deeper child includes.
//
// Filters holds the scoped filters of include=rel(field=value), with the
// same operator suffixes as top-level filters.
type IncludeNode struct {
	Name     string                // segment name (matches the relation's Name)
	Relation entity.Relation       // relation declared on the parent entity
	Target   *entity.Entity        // the entity Reached by following Relation
	Filters  []filter.ParsedFilter // scoped filters applied during eager-load
	// ReadScopes holds the target's Exposure.ReadScope predicates, kept apart
	// from Filters.
	ReadScopes []filter.ParsedFilter
	// HookScopes holds the clauses the target's own BeforeList, and for a
	// to-one relation BeforeGet, hooks appended.
	HookScopes []hook.WhereClause
	// Pivot is the registered entity owning a many_to_many's Through table,
	// nil when the migrator created it; PivotScopes and PivotHookScopes are
	// its own scope predicates and BeforeList clauses.
	Pivot           *entity.Entity
	PivotScopes     []filter.ParsedFilter
	PivotHookScopes []hook.WhereClause
	Children        []*IncludeNode // deeper includes, e.g. for "author.profile" the "profile" child of "author"
	scopedNodes     map[string]*IncludeNode
	childMap        map[string]*IncludeNode
}

// scopedClauseKey is the canonical spelling of a scoped-filter set.
func scopedClauseKey(parsed []filter.ParsedFilter) string {
	var b strings.Builder
	for _, f := range parsed {
		b.WriteString(f.Field)
		b.WriteByte(' ')
		b.WriteString(string(f.Op))
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(f.Value))
		if f.Op == filter.OpIn {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(f.Key()))
		}
		b.WriteByte(';')
	}
	return b.String()
}

// parseIncludeTreeQ parses ?include= ("author.profile,comments") into the
// roots of the include forest, resolving each segment against the registry.
func parseIncludeTreeQ(q url.Values, ent *entity.Entity, registry entity.Registry) ([]*IncludeNode, error) {
	raw := strings.TrimSpace(q.Get("include"))
	if raw == "" {
		return nil, nil
	}
	if registry == nil {
		return nil, i18n.NewError(reasonIncludeNoRegistry, map[string]any{"include": strconv.Quote(raw)})
	}

	var roots []*IncludeNode
	rootMap := map[string]*IncludeNode{}

	paths := splitIncludeDelim(raw, ',')
	if len(paths) > maxIncludePaths {
		return nil, i18n.NewError(reasonIncludeTooManyPaths, map[string]any{"count": len(paths), "max": maxIncludePaths})
	}

	for _, path := range paths {
		segments := splitIncludeDelim(path, '.')
		if len(segments) == 0 {
			continue
		}
		if len(segments) > maxIncludeDepth {
			return nil, i18n.NewError(reasonIncludeTooDeep, map[string]any{"include": strconv.Quote(path), "depth": len(segments), "max": maxIncludeDepth})
		}

		siblings := &roots
		siblingMap := rootMap
		currentEntity := ent

		for _, segRaw := range segments {
			seg, filterClause := splitSegmentFilter(segRaw)
			rel, ok := relationByName(currentEntity, seg)
			if !ok {
				if near := includeNearMiss(currentEntity, seg); near != "" {
					return nil, i18n.NewError(reasonUnknownIncludeHint, map[string]any{"include": strconv.Quote(path), "segment": strconv.Quote(seg), "entity": strconv.Quote(currentEntity.GetName()), "suggestion": strconv.Quote(near)})
				}
				return nil, i18n.NewError(reasonUnknownInclude, map[string]any{"include": strconv.Quote(path), "segment": strconv.Quote(seg), "entity": strconv.Quote(currentEntity.GetName())})
			}
			target, err := entity.ResolveTarget(registry, ent, rel.Entity)
			if err != nil {
				return nil, i18n.Wrap(reasonIncludeUnregistered, err, map[string]any{"include": strconv.Quote(path), "relation": strconv.Quote(seg), "entity": strconv.Quote(rel.Entity)})
			}
			if err := entity.CheckRelationKeys(currentEntity, rel, target); err != nil {
				return nil, i18n.Wrap(reasonIncludeCompositeKey, err, map[string]any{"include": strconv.Quote(path)})
			}
			node, exists := siblingMap[seg]
			if !exists {
				node = &IncludeNode{
					Name:        seg,
					Relation:    rel,
					Target:      target,
					childMap:    map[string]*IncludeNode{},
					scopedNodes: map[string]*IncludeNode{},
				}
				if rel.Type == entity.RelManyToMany {
					pivot, err := entity.PivotEntity(registry, currentEntity, rel.Through)
					if err != nil {
						return nil, i18n.Wrap(reasonIncludePivot, err, map[string]any{"include": strconv.Quote(path)})
					}
					node.Pivot = pivot
				}
				siblingMap[seg] = node
				*siblings = append(*siblings, node)
			}
			var parsed []filter.ParsedFilter
			if filterClause != "" {
				parsed, err = parseScopedFilters(filterClause, entity.ValueFields(registry, target), path)
				if err != nil {
					return nil, err
				}
			}
			key := scopedClauseKey(parsed)
			next, seen := node.scopedNodes[key]
			if !seen {
				next = node
				if exists {
					next = &IncludeNode{
						Name:     seg,
						Relation: rel,
						Target:   target,
						Pivot:    node.Pivot,
						childMap: map[string]*IncludeNode{},
					}
					*siblings = append(*siblings, next)
				}
				next.Filters = parsed
				node.scopedNodes[key] = next
			}
			siblings = &next.Children
			siblingMap = next.childMap
			currentEntity = target
		}
	}
	return roots, nil
}

// splitSegmentFilter splits "rel(filter=val)" into ("rel", "filter=val").
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

// splitIncludeDelim splits s on sep outside parentheses.
func splitIncludeDelim(s string, sep byte) []string {
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
		case sep:
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

// parseScopedFilters parses "status=published,body_like=foo" with the
// top-level suffix operators; nil fields accepts every name.
func parseScopedFilters(raw string, fields []schema.Field, pathForErrors string) ([]filter.ParsedFilter, error) {
	knownField := map[string]bool{}
	colType := map[string]schema.FieldType{}
	var noQueryField map[string]bool
	wireAlias := map[string]string{}
	for _, f := range fields {
		if f.NoQuery && !f.Hidden {
			if noQueryField == nil {
				noQueryField = map[string]bool{}
			}
			noQueryField[f.Name] = true
			if f.WireName != "" && f.WireName != f.Name {
				noQueryField[f.WireName] = true
			}
			continue
		}
		if !f.Hidden {
			knownField[f.Name] = true
			colType[f.Name] = f.Type
			if f.WireName != "" && f.WireName != f.Name {
				knownField[f.WireName] = true
				wireAlias[f.WireName] = f.Name
			}
		}
	}
	var out []filter.ParsedFilter
	for kv := range strings.SplitSeq(raw, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return nil, i18n.NewError(reasonScopedMissingEq, map[string]any{"include": strconv.Quote(pathForErrors), "filter": strconv.Quote(kv)})
		}
		key, value := kv[:eq], kv[eq+1:]
		named := func(n string) bool { return knownField[n] || noQueryField[n] }
		if fields == nil {
			named = func(n string) bool { return n != key }
		}
		field, op, _, err := filter.ReadKey(key, named)
		if err != nil {
			return nil, i18n.Wrap(reasonScopedFilter, err, map[string]any{"include": strconv.Quote(pathForErrors)})
		}
		if noQueryField[field] {
			return nil, i18n.NewError(reasonScopedNotFilterable, map[string]any{"include": strconv.Quote(pathForErrors), "field": strconv.Quote(field)})
		}
		if fields != nil && !knownField[field] {
			return nil, i18n.NewError(reasonScopedUnknownField, map[string]any{"include": strconv.Quote(pathForErrors), "field": strconv.Quote(field)})
		}
		sent := field
		if col, ok := wireAlias[field]; ok {
			field = col
		}
		if fields != nil {
			if err := filter.CheckOpType(sent, op, colType[field]); err != nil {
				return nil, i18n.Wrap(reasonScopedFilter, err, map[string]any{"include": strconv.Quote(pathForErrors)})
			}
		}
		if op == filter.OpIn {
			if n := strings.Count(value, "|") + 1; n > maxScopedINEntries {
				return nil, i18n.NewError(reasonScopedInListOverCap, map[string]any{"include": strconv.Quote(pathForErrors), "field": strconv.Quote(field), "count": n, "max": maxScopedINEntries})
			}
			vals := strings.SplitSeq(value, "|")
			for v := range vals {
				pf, err := scopedTyped(filter.ParsedFilter{Field: sent, Op: filter.OpIn, Value: v}, field, colType[field], pathForErrors)
				if err != nil {
					return nil, err
				}
				out = append(out, pf.InList(key))
			}
		} else {
			pf, err := scopedTyped(filter.ParsedFilter{Field: sent, Op: op, Value: value}, field, colType[field], pathForErrors)
			if err != nil {
				return nil, err
			}
			out = append(out, pf)
		}
	}
	return out, nil
}

// scopedTyped types pf's value for column type t and points it at column
// col; a mistyped value is an error naming the field the caller sent.
func scopedTyped(pf filter.ParsedFilter, col string, t schema.FieldType, pathForErrors string) (filter.ParsedFilter, error) {
	typed, err := pf.Typed(t)
	if err != nil {
		return filter.ParsedFilter{}, i18n.Wrap(reasonScopedFilter, err, map[string]any{"include": strconv.Quote(pathForErrors)})
	}
	typed.Field = col
	return typed, nil
}

const maxScopedINEntries = 256

// relationByName finds the relation a caller may name; one the entity
// hides (entity.HidesRelation) is not found.
func relationByName(ent *entity.Entity, name string) (entity.Relation, bool) {
	for _, rel := range ent.Config.Relations {
		if rel.Name == name && !ent.HidesRelation(rel) {
			return rel, true
		}
	}
	return entity.Relation{}, false
}

// includeNearMiss names the relation an unknown segment most likely meant
// (its foreign key or target entity), or "".
func includeNearMiss(ent *entity.Entity, seg string) string {
	for _, rel := range ent.Config.Relations {
		if ent.HidesRelation(rel) {
			continue
		}
		switch seg {
		case strings.TrimSuffix(rel.Name, "_id"), strings.TrimSuffix(oneFK(rel), "_id"),
			rel.Entity, strings.TrimSuffix(rel.Entity, "s"):
			return rel.Name
		}
	}
	return ""
}

// writeIncludeError renders an include failure: 400 for a blown row budget
// or a child hook's refusal, 403 for an unreadable target, else 500.
func writeIncludeError(w http.ResponseWriter, r *http.Request, surface string, err error) {
	if errors.Is(err, errIncludeBudget) {
		writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
		return
	}
	if forbidden, ok := errors.AsType[*includeForbiddenError](err); ok {
		writeErrJSON(w, r, http.StatusForbidden, forbidden, i18n.Phrase{})
		return
	}
	if refused, ok := errors.AsType[*childHookError](err); ok {
		writeErrJSON(w, r, http.StatusBadRequest, refused.err, i18n.Phrase{})
		return
	}
	logf("crud: %s include failed: %v", surface, err)
	writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
}

// applyIncludeTree loads the include forest onto rows, which carry wire
// keys. Nested rows keep raw column names until the last step converts
// everything attached to wire keys.
func (ch *CrudHandler) applyIncludeTree(ctx context.Context, rows []map[string]any, nodes []*IncludeNode) error {
	if len(nodes) == 0 {
		return nil
	}
	if _, overHTTP := ctx.Value(realRequestKey{}).(*http.Request); overHTTP {
		if err := ch.checkIncludeReadable(ctx, nodes); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		return nil
	}
	parentKey := ch.keyCols()
	ids := collectRowKeys(rows, parentKey, ch.convertKey)

	loaded := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		loaded[id] = make(map[string]any)
	}
	budget := newIncludeBudget()
	for _, node := range nodes {
		applyRelatedScope(ctx, node)
		applyRelatedReadScope(ctx, node)
		applyPivotScope(ctx, node)
		if err := ch.applyChildBeforeHooks(ctx, node); err != nil {
			return err
		}
		if err := loadIncludeNode(ctx, ch.conn(ctx), ch.Entity.GetTable(), parentKey, node, ids, loaded, budget); err != nil {
			if errors.Is(err, errIncludeBudget) {
				return err
			}
			return fmt.Errorf("eager load %s: %w", node.Relation.Name, err)
		}
		decodeEntityReadColumns(node.Target, gatherLoadedRows(loaded, node.Relation.Name))
	}
	dedupeRepeatedRelations(nodes, loaded)

	if err := ch.recurseIncludeChildren(ctx, nodes, loaded, budget); err != nil {
		return err
	}

	converted := map[uintptr]*convertedSubtree{}
	trees := ch.includeConvTrees(nodes)
	for _, row := range rows {
		id, ok := entity.RowKey(row, parentKey, ch.convertKey)
		if !ok {
			continue
		}
		bucket := loaded[id]
		for _, node := range nodes {
			outKey := ch.convertKey(node.Relation.Name)
			val, present := bucket[node.Relation.Name]
			out, err := ch.formatRelationValueDeep(node.Relation, trees[node.Relation.Name], val, present, converted, budget)
			if err != nil {
				return err
			}
			row[outKey] = out
		}
	}

	return ch.applyChildReadHooks(ctx, nodes, rows)
}

// recurseLoadOnRawRows loads children onto rawRows, which carry raw column
// names, and recurses.
func (ch *CrudHandler) recurseLoadOnRawRows(ctx context.Context, target *entity.Entity, children []*IncludeNode, rawRows []map[string]any, budget *includeBudget) error {
	key := target.KeyColumns()
	ids := collectRowKeys(rawRows, key, nil)
	if len(ids) == 0 {
		return nil
	}
	loaded := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		loaded[id] = make(map[string]any)
	}
	for _, node := range children {
		applyRelatedScope(ctx, node)
		applyRelatedReadScope(ctx, node)
		applyPivotScope(ctx, node)
		if err := ch.applyChildBeforeHooks(ctx, node); err != nil {
			return err
		}
		if err := loadIncludeNode(ctx, ch.conn(ctx), target.GetTable(), key, node, ids, loaded, budget); err != nil {
			if errors.Is(err, errIncludeBudget) {
				return err
			}
			return fmt.Errorf("eager load %s: %w", node.Relation.Name, err)
		}
		decodeEntityReadColumns(node.Target, gatherLoadedRows(loaded, node.Relation.Name))
	}
	dedupeRepeatedRelations(children, loaded)
	if err := ch.recurseIncludeChildren(ctx, children, loaded, budget); err != nil {
		return err
	}
	for _, row := range rawRows {
		id, ok := rawRowKey(row, key)
		if !ok {
			continue
		}
		bucket := loaded[id]
		for _, node := range children {
			val, present := bucket[node.Relation.Name]
			row[node.Relation.Name] = rawRelationValue(node.Relation, val, present)
		}
	}
	return nil
}

// dedupeRepeatedRelations keeps each child row once per parent, by key, for
// a relation the include names more than once.
func dedupeRepeatedRelations(nodes []*IncludeNode, loaded map[string]map[string]any) {
	mentions := make(map[string]int, len(nodes))
	for _, node := range nodes {
		mentions[node.Relation.Name]++
	}
	done := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		name := node.Relation.Name
		if mentions[name] < 2 || done[name] || node.Target == nil {
			continue
		}
		done[name] = true
		key := node.Target.KeyColumns()
		for _, bucket := range loaded {
			var children []map[string]any
			switch v := bucket[name].(type) {
			case []map[string]any:
				children = v
			default:
				continue
			}
			seen := make(map[string]bool, len(children))
			kept := make([]map[string]any, 0, len(children))
			for _, child := range children {
				if id, ok := rawRowKey(child, key); ok {
					if seen[id] {
						continue
					}
					seen[id] = true
				}
				kept = append(kept, child)
			}
			bucket[name] = kept
		}
	}
}

// recurseIncludeChildren loads each node's children onto its loaded rows.
func (ch *CrudHandler) recurseIncludeChildren(ctx context.Context, nodes []*IncludeNode, loaded map[string]map[string]any, budget *includeBudget) error {
	for _, node := range nodes {
		if len(node.Children) == 0 || node.Target == nil {
			continue
		}
		nestedRows := gatherLoadedRows(loaded, node.Relation.Name)
		if len(nestedRows) == 0 {
			continue
		}
		if err := ch.recurseLoadOnRawRows(ctx, node.Target, node.Children, nestedRows, budget); err != nil {
			return err
		}
	}
	return nil
}

// gatherLoadedRows flattens the rows loaded under relName, each map once.
func gatherLoadedRows(loaded map[string]map[string]any, relName string) []map[string]any {
	var out []map[string]any
	seen := map[uintptr]bool{}
	add := func(m map[string]any) {
		id := reflect.ValueOf(m).Pointer()
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, m)
	}
	for _, bucket := range loaded {
		v, ok := bucket[relName]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			add(x)
		case []map[string]any:
			for _, m := range x {
				add(m)
			}
		}
	}
	return out
}

// collectRowKeys returns the rows' record ids; nil names reads raw rows.
func collectRowKeys(rows []map[string]any, cols []string, names func(string) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if names == nil {
			if id, ok := rawRowKey(row, cols); ok {
				out = append(out, id)
			}
			continue
		}
		if id, ok := entity.RowKey(row, cols, names); ok {
			out = append(out, id)
		}
	}
	return out
}

// rawRowKey is a raw row's record id, its key columns matched ignoring case.
func rawRowKey(row map[string]any, cols []string) (string, bool) {
	return entity.RowKey(row, cols, func(c string) string {
		if _, ok := row[c]; ok {
			return c
		}
		for k := range row {
			if strings.EqualFold(k, c) {
				return k
			}
		}
		return c
	})
}

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

// convTree converts one include hop's keys, with the trees of the hops
// under it by relation name; documents names its schema.JSON columns.
type convTree struct {
	conv      func(string) string
	children  map[string]*convTree
	documents map[string]bool
}

func (ch *CrudHandler) hopTree(target *entity.Entity) *convTree {
	if target == nil {
		return &convTree{conv: ch.convertKey}
	}
	t := &convTree{conv: ch.wireConverterOf(target)}
	for _, f := range target.GetFields() {
		if f.Type == schema.JSON {
			if t.documents == nil {
				t.documents = map[string]bool{}
			}
			t.documents[f.Name] = true
		}
	}
	return t
}

// includeConvTrees builds each relation's converter tree, by relation name.
func (ch *CrudHandler) includeConvTrees(nodes []*IncludeNode) map[string]*convTree {
	out := map[string]*convTree{}
	for _, node := range nodes {
		var t *convTree
		if node.Target != nil {
			t = ch.hopTree(node.Target)
		} else {
			t = ch.hopTreeFor(node.Relation.Entity)
		}
		t.children = ch.includeConvTrees(node.Children)
		if prev, ok := out[node.Relation.Name]; ok {
			mergeConvTree(prev, t)
			continue
		}
		out[node.Relation.Name] = t
	}
	return out
}

func mergeConvTree(dst, src *convTree) {
	for k, v := range src.children {
		if d, ok := dst.children[k]; ok {
			mergeConvTree(d, v)
			continue
		}
		dst.children[k] = v
	}
}

// formatRelationValueDeep converts an attached relation value, and every
// subtree under it, to wire keys with tree.
func (ch *CrudHandler) formatRelationValueDeep(rel entity.Relation, tree *convTree, val any, present bool, converted map[uintptr]*convertedSubtree, budget *includeBudget) (any, error) {
	if tree == nil {
		tree = ch.hopTreeFor(rel.Entity)
	}
	switch rel.Type {
	case entity.RelHasMany, entity.RelManyToMany:
		if !present {
			return []map[string]any{}, nil
		}
		slice, ok := val.([]map[string]any)
		if !ok {
			return []map[string]any{}, nil
		}
		out := make([]map[string]any, len(slice))
		for i, m := range slice {
			c, _, err := ch.deepConvertMap(m, tree, converted, budget)
			if err != nil {
				return nil, err
			}
			out[i] = c.(map[string]any)
		}
		return out, nil
	default:
		if !present {
			return nil, nil
		}
		m, ok := val.(map[string]any)
		if !ok {
			return nil, nil
		}
		c, _, err := ch.deepConvertMap(m, tree, converted, budget)
		if err != nil {
			return nil, err
		}
		return c.(map[string]any), nil
	}
}

// deepConvertMap converts every map key under v with tree, memoised by map
// identity in converted, and returns the value and the row references it
// serialises to.
func (ch *CrudHandler) deepConvertMap(v any, tree *convTree, converted map[uintptr]*convertedSubtree, budget *includeBudget) (any, int, error) {
	if tree == nil || tree.conv == nil {
		tree = &convTree{conv: ch.convertKey}
	}
	switch x := v.(type) {
	case map[string]any:
		id := reflect.ValueOf(x).Pointer()
		if prev, ok := converted[id]; ok {
			if err := budget.spend(prev.nodes); err != nil {
				return nil, 0, err
			}
			return prev.out, prev.nodes, nil
		}
		if err := budget.spend(1); err != nil {
			return nil, 0, err
		}
		out := make(map[string]any, len(x))
		entry := &convertedSubtree{out: out, nodes: 1}
		converted[id] = entry
		total := 1
		for k, val := range x {
			if tree.documents[k] {
				out[tree.conv(k)] = val
				continue
			}
			sub := tree
			if child, ok := tree.children[k]; ok {
				sub = child
			}
			c, n, err := ch.deepConvertMap(val, sub, converted, budget)
			if err != nil {
				return nil, 0, err
			}
			total += n
			out[tree.conv(k)] = c
		}
		entry.nodes = total
		return out, total, nil
	case []map[string]any:
		out := make([]map[string]any, len(x))
		total := 0
		for i, m := range x {
			c, n, err := ch.deepConvertMap(m, tree, converted, budget)
			if err != nil {
				return nil, 0, err
			}
			total += n
			out[i] = c.(map[string]any)
		}
		return out, total, nil
	case []any:
		out := make([]any, len(x))
		total := 0
		for i, v := range x {
			c, n, err := ch.deepConvertMap(v, tree, converted, budget)
			if err != nil {
				return nil, 0, err
			}
			total += n
			out[i] = c
		}
		return out, total, nil
	default:
		return v, 0, nil
	}
}

// applyRelatedScope prepends the target's owner and tenant predicates to
// node.Filters.
func applyRelatedScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	node.Filters = append(eagerScopeFilters(ctx, node.Target), node.Filters...)
}

func applyPivotScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Pivot == nil {
		return
	}
	node.PivotScopes = pivotScopes(ctx, node.Pivot)
}

func applyRelatedReadScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	node.ReadScopes = readScopeFilters(ctx, node.Target)
}

// hopTreeFor is the converter tree of the entity targetName resolves to
// from this handler's version, or the handler's own when it resolves to none.
func (ch *CrudHandler) hopTreeFor(targetName string) *convTree {
	if ch.Registry == nil {
		return ch.hopTree(nil)
	}
	target, err := entity.ResolveTarget(ch.Registry, ch.Entity, targetName)
	if err != nil {
		return ch.hopTree(nil)
	}
	return ch.hopTree(target)
}

// wireConverterOf converts target's columns to the keys its included rows
// carry: a field's WireName, else this handler's JSON case.
func (ch *CrudHandler) wireConverterOf(target *entity.Entity) func(string) string {
	wire := make(map[string]string, len(target.GetFields()))
	for _, f := range target.GetFields() {
		if f.WireName != "" {
			wire[f.Name] = f.WireName
		}
	}
	if len(wire) == 0 {
		return ch.convertKeyRaw
	}
	return func(k string) string {
		if w, ok := wire[k]; ok {
			return w
		}
		return ch.convertKeyRaw(k)
	}
}

// includeForbiddenError is an include whose target the caller may not read.
type includeForbiddenError struct{ Entity string }

func (e *includeForbiddenError) Error() string {
	return reasonIncludeForbidden.Format(map[string]any{"entity": e.Entity})
}

func (e *includeForbiddenError) Phrased() (i18n.Phrase, map[string]any) {
	return reasonIncludeForbidden, map[string]any{"entity": e.Entity}
}

// checkIncludeReadable refuses the forest when any node's target, or its
// registered pivot, fails the read posture of its own list route.
func (ch *CrudHandler) checkIncludeReadable(ctx context.Context, nodes []*IncludeNode) error {
	for _, node := range nodes {
		if node == nil || node.Target == nil {
			continue
		}
		target := &CrudHandler{Entity: node.Target, DB: ch.DB, Registry: ch.Registry}
		if !target.relationReachable(ctx, "read") || !target.canReadEntityGate(ctx) {
			return &includeForbiddenError{Entity: node.Target.GetName()}
		}
		if err := ch.checkPivotReadable(ctx, node.Pivot); err != nil {
			return err
		}
		if err := ch.checkIncludeReadable(ctx, node.Children); err != nil {
			return err
		}
	}
	return nil
}

// checkPivotReadable refuses a registered join entity the caller may not
// read; nil passes.
func (ch *CrudHandler) checkPivotReadable(ctx context.Context, pivot *entity.Entity) error {
	if pivot == nil {
		return nil
	}
	probe := &CrudHandler{Entity: pivot, DB: ch.DB, Registry: ch.Registry}
	if !probe.relationReachable(ctx, "read") || !probe.canReadEntityGate(ctx) {
		return &includeForbiddenError{Entity: pivot.GetName()}
	}
	return nil
}
