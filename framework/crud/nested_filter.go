package crud

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/internal/casing"
)

var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isSafeIdentifier(s string) bool {
	return safeIdentifierRE.MatchString(s)
}

// isSafeTable reports whether s is an identifier or a schema-qualified one.
func isSafeTable(s string) bool {
	_, err := query.SafeIdent(s)
	return err == nil
}

// selfJoinAlias is a self-joining hop's alias: app.nodes becomes nodes_sub.
func selfJoinAlias(table string) string {
	return tableBase(table) + "_sub"
}

type relationHop struct {
	Relation   entity.Relation
	Target     *entity.Entity
	Table      string
	SoftDelete bool
	Scopes     []filter.ParsedFilter
	// Pivot owns a many_to_many hop's Through table; nil when the migrator made it.
	Pivot           *entity.Entity
	PivotScopes     []filter.ParsedFilter
	HookScopes      []hook.WhereClause
	PivotHookScopes []hook.WhereClause
}

// nestedFilter is one parsed ?author.name=alice predicate.
type nestedFilter struct {
	Hops       []relationHop
	Relation   entity.Relation
	Field      string
	Op         filter.FilterOp
	Value      string
	Values     []string
	colType    schema.FieldType
	softDelete bool
	scopes     []filter.ParsedFilter
	table      string
}

// parseNestedFiltersValues parses the dotted keys of q (?comments.post.author.name_like=al)
// into nested filters; an unknown relation or field is an error.
func parseNestedFiltersValues(q url.Values, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	if ent == nil {
		return nil, nil
	}
	var out []nestedFilter
	for _, key := range slices.Sorted(maps.Keys(q)) {
		values := q[key]
		if !strings.Contains(key, ".") || len(values) == 0 {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 2 {
			continue
		}
		if len(parts) > maxIncludeDepth+1 {
			return nil, i18n.NewError(reasonNestedTooManyHops, map[string]any{"key": strconv.Quote(key), "max": maxIncludeDepth})
		}

		currentEntity := ent
		var hops []relationHop
		for i := 0; i < len(parts)-1; i++ {
			relName := parts[i]
			rel, ok := relationByName(currentEntity, relName)
			if !ok {
				rel, ok = relationByName(currentEntity, casing.ToSnake(relName))
			}
			if !ok {
				return nil, i18n.NewError(reasonNestedUnknownRel, map[string]any{"key": strconv.Quote(key), "relation": strconv.Quote(relName)})
			}
			target, err := entity.ResolveTarget(registry, currentEntity, rel.Entity)
			if err != nil {
				return nil, i18n.Wrap(reasonNestedTargetFailed, err, map[string]any{"key": strconv.Quote(key), "target": strconv.Quote(rel.Entity)})
			}
			if target == nil {
				return nil, i18n.NewError(reasonNestedTargetMissing, map[string]any{"key": strconv.Quote(key), "target": strconv.Quote(rel.Entity)})
			}
			if err := entity.CheckRelationKeys(currentEntity, rel, target); err != nil {
				return nil, i18n.Wrap(reasonNestedCompositeKey, err, map[string]any{"key": strconv.Quote(key)})
			}
			hop, err := newRelationHop(registry, currentEntity, rel, target)
			if err != nil {
				return nil, i18n.Wrap(reasonNestedTargetFailed, err, map[string]any{"key": strconv.Quote(key), "target": strconv.Quote(rel.Entity)})
			}
			hops = append(hops, hop)
			currentEntity = target
		}

		fieldName, op, err := splitFieldOp(currentEntity, parts[len(parts)-1])
		if err != nil {
			return nil, i18n.Wrap(reasonNestedFilter, err, map[string]any{"key": strconv.Quote(key)})
		}

		if !isSafeIdentifier(fieldName) {
			return nil, i18n.NewError(reasonNestedUnsafeField, map[string]any{"key": strconv.Quote(key)})
		}

		target := currentEntity
		sent := fieldName
		known, blocked := false, false
		var colType schema.FieldType
		if f, ok := nestedField(registry, target, fieldName); ok {
			known = !f.Hidden
			blocked = known && f.NoQuery
			if known {
				fieldName = f.Name
				colType = f.Type
			}
		}
		if blocked {
			return nil, i18n.NewError(reasonNestedNotFilterable, map[string]any{"key": strconv.Quote(key), "field": strconv.Quote(fieldName)})
		}
		if !known {
			return nil, i18n.NewError(reasonNestedUnknownField, map[string]any{"key": strconv.Quote(key), "field": strconv.Quote(fieldName), "entity": strconv.Quote(target.GetName())})
		}
		if err := filter.CheckOpType(fieldName, op, colType); err != nil {
			return nil, i18n.Wrap(reasonNestedFilter, err, map[string]any{"key": strconv.Quote(key)})
		}

		nf := nestedFilter{
			Hops:       hops,
			Relation:   hops[0].Relation,
			Field:      fieldName,
			Op:         op,
			colType:    colType,
			softDelete: hops[0].SoftDelete,
			table:      hops[0].Table,
		}
		if op == filter.OpIn {
			vals, total := filter.SplitINValuesBounded(values, filter.MaxINListEntries)
			if total > filter.MaxINListEntries {
				return nil, i18n.NewError(reasonNestedInListOverCap, map[string]any{
					"key": strconv.Quote(key), "field": strconv.Quote(fieldName), "count": total, "max": filter.MaxINListEntries,
				})
			}
			nf.Values = vals
		} else {
			if len(values) > 1 {
				return nil, filter.RepeatedFilterError(key, len(values))
			}
			nf.Value = values[0]
		}
		if err := checkNestedValues(sent, nf); err != nil {
			return nil, i18n.Wrap(reasonNestedFilter, err, map[string]any{"key": strconv.Quote(key)})
		}
		out = append(out, nf)
	}
	return out, nil
}

// nestedField finds the field name names on target, by column, wire or
// camelCase name, Hidden fields included.
func nestedField(registry entity.Registry, target *entity.Entity, name string) (schema.Field, bool) {
	for _, f := range entity.ValueFields(registry, target) {
		if f.Name == name || (f.WireName != "" && f.WireName == name) || casing.ToSnake(name) == f.Name {
			return f, true
		}
	}
	return schema.Field{}, false
}

// splitFieldOp splits a nested key's last segment into field and operator
// as filter.ReadKey splits a top-level key.
func splitFieldOp(target *entity.Entity, raw string) (string, filter.FilterOp, error) {
	visible := func(name string) bool {
		f, ok := nestedField(nil, target, name)
		return ok && !f.Hidden
	}
	name, op, _, err := filter.ReadKey(raw, visible)
	return name, op, err
}

// checkNestedValues refuses a value filter.CoerceValue refuses for the
// target column; a like value is checked as text.
func checkNestedValues(sent string, nf nestedFilter) error {
	switch nf.Op {
	case filter.OpLike:
		_, err := filter.CoerceValue(sent, schema.String, nf.Value)
		return err
	case filter.OpIn:
		for _, v := range nf.Values {
			if _, err := filter.CoerceValue(sent, nf.colType, v); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := filter.CoerceValue(sent, nf.colType, nf.Value)
		return err
	}
}

// NestedFilter is the in-process form of ?author.name=alice. Relation is a
// relation path on the parent entity and Field a field of its target. For
// OpIn set Values; Value is ignored.
type NestedFilter struct {
	Relation string
	Field    string
	Op       filter.FilterOp
	Value    string
	Values   []string
}

// resolveNestedFilters is parseNestedFiltersValues for in-process specs.
func resolveNestedFilters(ent *entity.Entity, registry entity.Registry, specs []NestedFilter) ([]nestedFilter, error) {
	if ent == nil || len(specs) == 0 {
		return nil, nil
	}
	out := make([]nestedFilter, 0, len(specs))
	for _, spec := range specs {
		relParts := strings.Split(spec.Relation, ".")
		if len(relParts) > maxIncludeDepth {
			return nil, fmt.Errorf("nested filter %q: too many relation hops (max %d)", spec.Relation, maxIncludeDepth)
		}

		currentEntity := ent
		var hops []relationHop
		for _, relName := range relParts {
			rel, ok := relationByName(currentEntity, relName)
			if !ok {
				rel, ok = relationByName(currentEntity, casing.ToSnake(relName))
			}
			if !ok {
				return nil, fmt.Errorf("nested filter: unknown relation %q", relName)
			}
			target, err := entity.ResolveTarget(registry, currentEntity, rel.Entity)
			if err != nil {
				return nil, fmt.Errorf("nested filter %q.%q: cannot resolve relation target %q: %w",
					spec.Relation, spec.Field, rel.Entity, err)
			}
			if target == nil {
				return nil, fmt.Errorf("nested filter %q.%q: relation target %q resolved to no entity",
					spec.Relation, spec.Field, rel.Entity)
			}
			if err := entity.CheckRelationKeys(currentEntity, rel, target); err != nil {
				return nil, fmt.Errorf("nested filter %q.%q: %w", spec.Relation, spec.Field, err)
			}
			hop, err := newRelationHop(registry, currentEntity, rel, target)
			if err != nil {
				return nil, fmt.Errorf("nested filter %q.%q: %w", spec.Relation, spec.Field, err)
			}
			hops = append(hops, hop)
			currentEntity = target
		}

		field := spec.Field
		if !isSafeIdentifier(field) {
			return nil, fmt.Errorf("nested filter %q.%q: unsafe field name", spec.Relation, spec.Field)
		}

		target := currentEntity
		known, blocked := false, false
		var colType schema.FieldType
		if f, ok := nestedField(registry, target, field); ok {
			known = !f.Hidden
			blocked = known && f.NoQuery
			if known {
				field = f.Name
				colType = f.Type
			}
		}
		if blocked {
			return nil, fmt.Errorf("nested filter %q.%q: field cannot be filtered", spec.Relation, spec.Field)
		}
		if !known {
			return nil, fmt.Errorf("nested filter %q.%q: field not declared on %q", spec.Relation, spec.Field, target.GetName())
		}
		op := spec.Op
		if op == "" {
			op = filter.OpEq
		}
		switch op {
		case filter.OpEq, filter.OpNe, filter.OpGt, filter.OpGte, filter.OpLt, filter.OpLte, filter.OpLike, filter.OpIn:
		default:
			return nil, fmt.Errorf("nested filter %q.%q: unsupported operator %q", spec.Relation, spec.Field, spec.Op)
		}
		if err := filter.CheckOpType(spec.Field, op, colType); err != nil {
			return nil, fmt.Errorf("nested filter %q: %w", spec.Relation, err)
		}
		nf := nestedFilter{
			Hops:       hops,
			Relation:   hops[0].Relation,
			Field:      field,
			Op:         op,
			colType:    colType,
			softDelete: hops[0].SoftDelete,
			table:      hops[0].Table,
		}
		if op == filter.OpIn {
			if len(spec.Values) > filter.MaxINListEntries {
				return nil, fmt.Errorf("nested filter %q.%q: in-list has %d entries (max %d)",
					spec.Relation, field, len(spec.Values), filter.MaxINListEntries)
			}
			nf.Values = spec.Values
		} else {
			nf.Value = spec.Value
		}
		if err := checkNestedValues(spec.Field, nf); err != nil {
			return nil, fmt.Errorf("nested filter %q: %w", spec.Relation, err)
		}
		out = append(out, nf)
	}
	return out, nil
}

// applyNestedFilters adds one EXISTS subquery per filter through addWhere.
func applyNestedFilters(addWhere func(sql string, args ...any), parentTable string, parentKey []string, filters []nestedFilter) {
	for _, nf := range filters {
		sql, args := buildExistsSubquery(parentTable, parentKey, nf)
		addWhere(sql, args...)
	}
}

// buildExistsSubquery returns the WHERE fragment for one nested filter;
// its $N placeholders are renumbered by the outer builder.
func buildExistsSubquery(parentTable string, parentKey []string, nf nestedFilter) (string, []any) {
	hops := nf.Hops
	if len(hops) == 0 {
		relTable := nf.table
		if relTable == "" {
			relTable = nf.Relation.Entity
		}
		hops = []relationHop{{
			Relation:   nf.Relation,
			Table:      relTable,
			SoftDelete: nf.softDelete,
			Scopes:     nf.scopes,
		}}
	}

	col := nf.Field
	if !isSafeIdentifier(col) {
		return "1 = 0", nil
	}

	for _, hop := range hops {
		for _, sc := range hop.Scopes {
			if !isSafeIdentifier(sc.Field) {
				return "1 = 0", nil
			}
		}
	}

	return buildHopSubquery(parentTable, parentKey, hops, nf)
}

// keyJoin renders "lt.a = rt.x AND lt.b = rt.y" for lcols and rcols.
func keyJoin(lt string, lcols []string, rt string, rcols []string) string {
	conds := make([]string, len(lcols))
	for i := range lcols {
		conds[i] = query.QuoteKeyword(lt+"."+lcols[i]) + " = " + query.QuoteKeyword(rt+"."+rcols[i])
	}
	return strings.Join(conds, " AND ")
}

// safeIdentifiers reports whether cols is non-empty and all plain identifiers.
func safeIdentifiers(cols []string) bool {
	return len(cols) > 0 && !slices.ContainsFunc(cols, func(c string) bool { return !isSafeIdentifier(c) })
}

func buildHopSubquery(parentTable string, parentKey []string, hops []relationHop, nf nestedFilter) (string, []any) {
	if len(hops) == 0 {
		return "1 = 1", nil
	}

	hop := hops[0]
	rel := hop.Relation
	relTable := hop.Table
	if relTable == "" {
		relTable = rel.Entity
	}
	targetKey := hop.Target.KeyColumns()
	fks := rel.ForeignKey
	var joinable bool
	switch rel.Type {
	case entity.RelManyToOne:
		joinable = len(fks) == len(targetKey)
	case entity.RelHasOne, entity.RelHasMany:
		joinable = len(fks) == len(parentKey)
	case entity.RelManyToMany:
		joinable = len(parentKey) == 1 && len(targetKey) == 1
	}
	if !joinable || !isSafeTable(parentTable) || !safeIdentifiers(parentKey) || !isSafeTable(relTable) || !safeIdentifiers(targetKey) {
		return "1 = 0", nil
	}
	if rel.Type != entity.RelManyToMany && !safeIdentifiers(fks) {
		return "1 = 0", nil
	}

	subTable := relTable
	aliasClause := query.QuoteKeyword(relTable)
	if relTable == parentTable {
		subTable = selfJoinAlias(relTable)
		aliasClause = fmt.Sprintf("%s %s", query.QuoteKeyword(relTable), query.QuoteKeyword(subTable))
	}

	var args []any
	var scopeClause string
	if len(hop.Scopes) > 0 {
		var scopeArgs []any
		scopeClause, scopeArgs = renderReadScope(hop.Scopes, subTable, 1)
		if scopeClause == "" {
			return "1 = 0", nil
		}
		args = append(args, scopeArgs...)
	}
	if hookPred, hookArgs := hookScopeSubquery(subTable, relTable, targetKey, hop.HookScopes); hookPred != "" {
		if scopeClause != "" {
			scopeClause += " AND "
		}
		scopeClause += hookPred
		args = append(args, hookArgs...)
	}

	var innerPredicate string
	if len(hops) == 1 {
		col := nf.Field
		var fieldPred string
		if nf.Op == filter.OpIn {
			if len(nf.Values) == 0 {
				return "1 = 0", nil
			}
			ph := make([]string, len(nf.Values))
			for i, v := range nf.Values {
				ph[i] = fmt.Sprintf("$%d", len(args)+1)
				args = append(args, filter.TypedBind(nf.colType, v))
			}
			fieldPred = fmt.Sprintf("%s IN (%s)", query.QuoteKeyword(subTable+"."+col), strings.Join(ph, ","))
		} else if nf.Op == filter.OpLike {
			var arg string
			fieldPred, arg = filter.LikeClause(subTable+"."+col, len(args)+1, nf.Value)
			args = append(args, arg)
		} else {
			fieldPred = fmt.Sprintf("%s %s $%d", query.QuoteKeyword(subTable+"."+col), opToSQL(nf.Op), len(args)+1)
			args = append(args, filter.TypedBind(nf.colType, nf.Value))
		}
		innerPredicate = fieldPred
	} else {
		subSQL, subArgs := buildHopSubquery(subTable, targetKey, hops[1:], nf)
		args = append(args, subArgs...)
		innerPredicate = subSQL
	}

	if scopeClause != "" {
		innerPredicate = scopeClause + " AND " + innerPredicate
	}
	if hop.SoftDelete {
		innerPredicate = fmt.Sprintf("%s.deleted_at IS NULL AND %s", query.QuoteKeyword(subTable), innerPredicate)
	}

	switch rel.Type {
	case entity.RelManyToOne:
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s AND %s)",
			aliasClause, keyJoin(subTable, targetKey, parentTable, fks), innerPredicate,
		), args
	case entity.RelHasOne, entity.RelHasMany:
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s AND %s)",
			aliasClause, keyJoin(subTable, fks, parentTable, parentKey), innerPredicate,
		), args
	case entity.RelManyToMany:
		if !isSafeTable(rel.Through) || !isSafeIdentifier(rel.ForeignKeyTarget) || !isSafeIdentifier(rel.LocalKey) {
			return "1 = 0", nil
		}
		pivotWhere, pivotArgs := pivotClause(hop.Pivot, hop.PivotScopes, rel.Through, len(args)+1)
		args = append(args, pivotArgs...)
		if hookPred, hookArgs := pivotHookScope(hop.Pivot, rel.Through, hop.PivotHookScopes); hookPred != "" {
			pivotWhere += " AND " + hookPred
			args = append(args, hookArgs...)
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s JOIN %s ON %s = %s WHERE %s = %s AND %s%s)",
			aliasClause, query.QuoteKeyword(rel.Through),
			query.QuoteKeyword(subTable+"."+targetKey[0]), query.QuoteKeyword(rel.Through+"."+rel.ForeignKeyTarget),
			query.QuoteKeyword(rel.Through+"."+rel.LocalKey), query.QuoteKeyword(parentTable+"."+parentKey[0]),
			innerPredicate,
			pivotWhere,
		), args
	default:
		return "1 = 0", nil
	}
}

func opToSQL(op filter.FilterOp) string {
	switch op {
	case filter.OpEq:
		return "="
	case filter.OpNe:
		return "!="
	case filter.OpGt:
		return ">"
	case filter.OpGte:
		return ">="
	case filter.OpLt:
		return "<"
	case filter.OpLte:
		return "<="
	case filter.OpLike:
		return "LIKE"
	case filter.OpIn:
		return "="
	}
	return "="
}

// scopeNestedFiltersForCaller narrows every filter to the rows the caller
// may read and refuses a target they may not read at all.
func (ch *CrudHandler) scopeNestedFiltersForCaller(ctx context.Context, filters []nestedFilter) error {
	return ch.scopeNestedFilters(ctx, filters, true)
}

// scopeNestedFilters narrows every filter to the caller's owner, tenant,
// read and hook scopes; checkPosture also asks whether they may read each target.
func (ch *CrudHandler) scopeNestedFilters(ctx context.Context, filters []nestedFilter, checkPosture bool) error {
	if len(filters) == 0 {
		return nil
	}
	if ch.Registry == nil {
		return fmt.Errorf("nested filter: registry required for scoping")
	}
	for i := range filters {
		if len(filters[i].Hops) == 0 {
			target, err := entity.ResolveTarget(ch.Registry, ch.Entity, filters[i].Relation.Entity)
			if err != nil || target == nil {
				return &includeForbiddenError{Entity: filters[i].Relation.Entity}
			}
			probe := ch.forEntity(target)
			if checkPosture && (!probe.relationReachable(ctx, "read") || !probe.CanReadScoped(ctx)) {
				return &includeForbiddenError{Entity: target.GetName()}
			}
			var scopes []filter.ParsedFilter
			scopes = append(scopes, eagerScopeFilters(ctx, target)...)
			scopes = append(scopes, readScopeFilters(ctx, target)...)
			filters[i].scopes = scopes
			continue
		}

		parent := ch.Entity
		for h := range filters[i].Hops {
			target := filters[i].Hops[h].Target
			if target == nil {
				var err error
				target, err = entity.ResolveTarget(ch.Registry, parent, filters[i].Hops[h].Relation.Entity)
				if err != nil || target == nil {
					return &includeForbiddenError{Entity: filters[i].Hops[h].Relation.Entity}
				}
				filters[i].Hops[h].Target = target
			}
			probe := ch.forEntity(target)
			if checkPosture && (!probe.relationReachable(ctx, "read") || !probe.CanReadScoped(ctx)) {
				return &includeForbiddenError{Entity: target.GetName()}
			}
			if checkPosture {
				if err := ch.checkPivotReadable(ctx, filters[i].Hops[h].Pivot); err != nil {
					return err
				}
			}
			var scopes []filter.ParsedFilter
			scopes = append(scopes, eagerScopeFilters(ctx, target)...)
			scopes = append(scopes, readScopeFilters(ctx, target)...)
			filters[i].Hops[h].Scopes = scopes
			filters[i].Hops[h].PivotScopes = pivotScopes(ctx, filters[i].Hops[h].Pivot)
			if checkPosture || readHooksEnabled(ctx) {
				where, err := ch.childBeforeScopes(ctx, target, filters[i].Hops[h].Relation)
				if err != nil {
					return err
				}
				filters[i].Hops[h].HookScopes = where
				pivotWhere, err := ch.childBeforeScopes(ctx, filters[i].Hops[h].Pivot, filters[i].Hops[h].Relation)
				if err != nil {
					return err
				}
				filters[i].Hops[h].PivotHookScopes = pivotWhere
			}
			if h == 0 {
				filters[i].scopes = scopes
			}
			parent = target
		}
	}
	return nil
}

func newRelationHop(registry entity.Registry, source *entity.Entity, rel entity.Relation, target *entity.Entity) (relationHop, error) {
	hop := relationHop{
		Relation:   rel,
		Target:     target,
		Table:      resolvedTable(target, rel),
		SoftDelete: target.Config.Scope.SoftDelete,
	}
	if rel.Type == entity.RelManyToMany {
		pivot, err := entity.PivotEntity(registry, source, rel.Through)
		if err != nil {
			return relationHop{}, err
		}
		hop.Pivot = pivot
	}
	return hop, nil
}

func resolvedTable(target *entity.Entity, rel entity.Relation) string {
	if target != nil && target.GetTable() != "" {
		return target.GetTable()
	}
	return rel.Entity
}

func (ch *CrudHandler) scopeNestedFiltersInProcess(ctx context.Context, filters []nestedFilter) error {
	if len(filters) == 0 {
		return nil
	}
	return ch.scopeNestedFilters(ctx, filters, false)
}
