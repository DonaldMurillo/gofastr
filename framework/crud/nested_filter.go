package crud

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// safeIdentifierRE constrains nested-filter field names to a SQL-safe
// identifier shape: letter or underscore start, letters/digits/underscore
// continuation. Anything containing whitespace, quotes, semicolons,
// parentheses, comment markers, or operators is rejected outright.
// Field names come from query-string keys (?author.name OR 1=1 -- = foo)
// and must NEVER be embedded into SQL verbatim.
//
// Deliberately NOT core/query.identRe: that regex additionally allows
// dot-separated schema.table paths, while a nested-filter FIELD half
// must be a single segment (multi-level paths are rejected by design
// above), so the tighter single-segment shape is load-bearing here.
// The two regexes differ on purpose; unifying them would loosen this
// allow-list.
var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// isSafeIdentifier reports whether s is a plain SQL identifier (letters,
// digits, underscores, leading non-digit).
func isSafeIdentifier(s string) bool {
	return safeIdentifierRE.MatchString(s)
}

// relationHop describes one hop in a multi-level relation traversal.
type relationHop struct {
	Relation   entity.Relation
	Target     *entity.Entity
	Table      string
	SoftDelete bool
	Scopes     []filter.ParsedFilter
}

// nestedFilter is one parsed `?author.name=alice` or `?author.team.name=x` style predicate.
type nestedFilter struct {
	Hops       []relationHop
	Relation   entity.Relation // 1-hop fallback
	Field      string
	Op         filter.FilterOp
	Value      string   // single-value ops (eq/gt/like/…)
	Values     []string // OpIn: the full value set, emitted as one IN (...)
	isBool     bool
	softDelete bool                  // 1-hop fallback
	scopes     []filter.ParsedFilter // 1-hop fallback
	table      string                // 1-hop fallback
}

// parseNestedFilters extracts dotted-path query params and resolves their
// relation references against the entity's declared relations. Multi-hop
// nesting is supported up to depth 4 (e.g. `?comments.post.author.name=alice`).
//
// Suffixes (_gt/_gte/_lt/_lte/_like/_in) mirror ParseFilters semantics, but
// the suffix applies to the FIELD half, not the relation half:
//
//	?author.name_like=al%        ok
//	?author_like.name=al         not supported
//
// Unknown relations and unknown fields on the target return an error so
// the caller can map to 400, silent ignoring would mask client typos.
func parseNestedFilters(r *http.Request, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	return parseNestedFiltersValues(r.URL.Query(), ent, registry)
}

func parseNestedFiltersValues(q url.Values, ent *entity.Entity, registry entity.Registry) ([]nestedFilter, error) {
	if ent == nil {
		return nil, nil
	}
	var out []nestedFilter
	for key, values := range q {
		if !strings.Contains(key, ".") || len(values) == 0 {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) < 2 {
			continue
		}
		if len(parts) > maxIncludeDepth+1 {
			return nil, fmt.Errorf("nested filter %q: too many relation hops (max %d)", key, maxIncludeDepth)
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
				return nil, fmt.Errorf("nested filter %q: unknown relation %q", key, relName)
			}
			target, err := entity.ResolveTarget(registry, currentEntity, rel.Entity)
			if err != nil {
				return nil, fmt.Errorf("nested filter %q: cannot resolve relation target %q: %w", key, rel.Entity, err)
			}
			if target == nil {
				return nil, fmt.Errorf("nested filter %q: relation target %q resolved to no entity", key, rel.Entity)
			}
			hops = append(hops, relationHop{
				Relation:   rel,
				Target:     target,
				Table:      resolvedTable(target, rel),
				SoftDelete: target.Config.Scope.SoftDelete,
			})
			currentEntity = target
		}

		fieldRaw := parts[len(parts)-1]
		fieldName := fieldRaw
		op := filter.OpEq
		for _, s := range filter.FilterSuffixes {
			if before, ok0 := strings.CutSuffix(fieldRaw, s.Suffix); ok0 {
				fieldName = before
				op = s.Op
				break
			}
		}

		// Refuse field names that aren't plain SQL identifiers.
		if !isSafeIdentifier(fieldName) {
			return nil, fmt.Errorf("nested filter %q: unsafe field name", key)
		}

		target := currentEntity
		known, blocked, isBool := false, false, false
		for _, f := range target.GetFields() {
			if f.Name == fieldName || (f.WireName != "" && f.WireName == fieldName) || casing.ToSnake(fieldName) == f.Name {
				known = !f.Hidden
				blocked = known && f.NoQuery
				if known {
					fieldName = f.Name // rewrite to the column: this reaches SQL
					isBool = f.Type == schema.Bool
				}
				break
			}
		}
		if blocked {
			return nil, fmt.Errorf("nested filter %q: field %q cannot be filtered", key, fieldName)
		}
		if !known {
			return nil, fmt.Errorf("nested filter %q: field %q not declared on %q", key, fieldName, target.GetName())
		}

		nf := nestedFilter{
			Hops:       hops,
			Relation:   hops[0].Relation,
			Field:      fieldName,
			Op:         op,
			isBool:     isBool,
			softDelete: hops[0].SoftDelete,
			table:      hops[0].Table,
		}
		if op == filter.OpIn {
			vals, total := filter.SplitINValuesBounded(values, filter.MaxINListEntries)
			if total > filter.MaxINListEntries {
				return nil, fmt.Errorf("nested filter %q: in-list on %q has %d entries (max %d)",
					key, fieldName, total, filter.MaxINListEntries)
			}
			nf.Values = vals
		} else {
			nf.Value = values[0]
		}
		out = append(out, nf)
	}
	return out, nil
}

// NestedFilter is the in-process (ListOptions) equivalent of a single
// `?author.name=alice` HTTP query param. Typed repositories construct these
// directly instead of synthesising a URL. Relation names the declared
// relation on the parent entity; Field is the column on the target entity;
// Op/Value mirror ParsedFilter semantics. For Op==OpIn, set Values (Value is
// ignored).
type NestedFilter struct {
	Relation string
	Field    string
	Op       filter.FilterOp
	Value    string
	Values   []string
}

// resolveNestedFilters maps in-process NestedFilter specs onto the internal
// nestedFilter slice consumed by applyNestedFilters, running the same
// relation/field validation and identifier-safety checks the HTTP path
// applies in parseNestedFilters. Unknown relations, unknown fields, and
// unsafe identifiers return an error so typed callers see the same 400-class
// failures.
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
			hops = append(hops, relationHop{
				Relation:   rel,
				Target:     target,
				Table:      resolvedTable(target, rel),
				SoftDelete: target.Config.Scope.SoftDelete,
			})
			currentEntity = target
		}

		field := spec.Field
		if !isSafeIdentifier(field) {
			return nil, fmt.Errorf("nested filter %q.%q: unsafe field name", spec.Relation, spec.Field)
		}

		target := currentEntity
		known, blocked, isBool := false, false, false
		for _, f := range target.GetFields() {
			if f.Name == field || (f.WireName != "" && f.WireName == field) || casing.ToSnake(field) == f.Name {
				known = !f.Hidden
				blocked = known && f.NoQuery
				if known {
					field = f.Name // rewrite to the column: this reaches SQL
					isBool = f.Type == schema.Bool
				}
				break
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
		case filter.OpEq, filter.OpGt, filter.OpGte, filter.OpLt, filter.OpLte, filter.OpLike, filter.OpIn:
			// valid
		default:
			return nil, fmt.Errorf("nested filter %q.%q: unsupported operator %q", spec.Relation, spec.Field, spec.Op)
		}
		nf := nestedFilter{
			Hops:       hops,
			Relation:   hops[0].Relation,
			Field:      field,
			Op:         op,
			isBool:     isBool,
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
		out = append(out, nf)
	}
	return out, nil
}

// applyNestedFilters invokes addWhere once per nestedFilter with an EXISTS
// subquery. EXISTS avoids the row duplication that a plain JOIN would
// introduce for HasMany / ManyToMany relations and works uniformly across
// BelongsTo / HasOne too, same SQL pattern, no per-relation special-casing.
//
// addWhere mirrors the Where signature shared by QueryBuilder and
// CountBuilder so a single call site can wire the same filter chain into
// both the data and count queries.
func applyNestedFilters(addWhere func(sql string, args ...any), parentTable, parentPK string, filters []nestedFilter) {
	for _, nf := range filters {
		sql, args := buildExistsSubquery(parentTable, parentPK, nf)
		addWhere(sql, args...)
	}
}

// buildExistsSubquery returns the WHERE fragment for one nested filter.
// Renumbering happens inside QueryBuilder.Build, the args are passed
// through carry semantics that make $N adjustment correct downstream.
//
// The field name on the target relation comes from a URL query key
// (?author.name=...) and is interpolated into the SQL directly, there
// is no parameter placeholder for an identifier. We refuse anything
// that doesn't look like a plain `[A-Za-z_][A-Za-z0-9_]*` identifier so
// payloads like `name OR 1=1 --` can't smuggle SQL fragments through
// parseNestedFilters when the registry can't validate the field.
//
// Relation metadata (parentTable, parentPK, rel.Entity, rel.ForeignKey, rel.Through,
// rel.LocalKey, rel.ForeignKeyTarget) may originate from dynamic definitions or API endpoints
// (e.g. kiln add_entity/update_entity), so defense-in-depth isSafeIdentifier gates apply
// across buildHopSubquery as well.
func buildExistsSubquery(parentTable, parentPK string, nf nestedFilter) (string, []any) {
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

	return buildHopSubquery(parentTable, parentPK, hops, nf)
}

func buildHopSubquery(parentTable, parentPK string, hops []relationHop, nf nestedFilter) (string, []any) {
	if len(hops) == 0 {
		return "1 = 1", nil
	}

	hop := hops[0]
	rel := hop.Relation
	relTable := hop.Table
	if relTable == "" {
		relTable = rel.Entity
	}
	targetPK := "id"
	if hop.Target != nil && hop.Target.PrimaryKey != "" {
		targetPK = hop.Target.PrimaryKey
	}

	if parentPK == "" {
		parentPK = "id"
	}

	if !isSafeIdentifier(parentTable) || !isSafeIdentifier(parentPK) || !isSafeIdentifier(relTable) || !isSafeIdentifier(targetPK) {
		return "1 = 0", nil
	}

	subTable := relTable
	aliasClause := relTable
	if relTable == parentTable {
		subTable = relTable + "_sub"
		aliasClause = fmt.Sprintf("%s %s", relTable, subTable)
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
				args = append(args, filter.BoolBind(nf.isBool, v))
			}
			fieldPred = fmt.Sprintf("%s.%s IN (%s)", subTable, col, strings.Join(ph, ","))
		} else if nf.Op == filter.OpLike {
			fieldPred = fmt.Sprintf("%s.%s LIKE $%d"+filter.LikeEscapeSuffix, subTable, col, len(args)+1)
			args = append(args, filter.EscapeLikePattern(nf.Value))
		} else {
			fieldPred = fmt.Sprintf("%s.%s %s $%d", subTable, col, opToSQL(nf.Op), len(args)+1)
			args = append(args, filter.BoolBind(nf.isBool, nf.Value))
		}
		innerPredicate = fieldPred
	} else {
		subSQL, subArgs := buildHopSubquery(subTable, targetPK, hops[1:], nf)
		args = append(args, subArgs...)
		innerPredicate = subSQL
	}

	if scopeClause != "" {
		innerPredicate = scopeClause + " AND " + innerPredicate
	}
	if hop.SoftDelete {
		innerPredicate = fmt.Sprintf("%s.deleted_at IS NULL AND %s", subTable, innerPredicate)
	}

	switch rel.Type {
	case entity.RelManyToOne:
		if !isSafeIdentifier(rel.ForeignKey) {
			return "1 = 0", nil
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.%s AND %s)",
			aliasClause, subTable, targetPK, parentTable, rel.ForeignKey, innerPredicate,
		), args
	case entity.RelHasOne, entity.RelHasMany:
		if !isSafeIdentifier(rel.ForeignKey) {
			return "1 = 0", nil
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.%s AND %s)",
			aliasClause, subTable, rel.ForeignKey, parentTable, parentPK, innerPredicate,
		), args
	case entity.RelManyToMany:
		if !isSafeIdentifier(rel.Through) || !isSafeIdentifier(rel.ForeignKeyTarget) || !isSafeIdentifier(rel.LocalKey) {
			return "1 = 0", nil
		}
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM %s JOIN %s ON %s.%s = %s.%s WHERE %s.%s = %s.%s AND %s)",
			aliasClause, rel.Through,
			subTable, targetPK, rel.Through, rel.ForeignKeyTarget,
			rel.Through, rel.LocalKey, parentTable, parentPK,
			innerPredicate,
		), args
	default:
		return "1 = 0", nil
	}
}

// opToSQL maps a FilterOp to its SQL operator.
func opToSQL(op filter.FilterOp) string {
	switch op {
	case filter.OpEq:
		return "="
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
		// OpIn is handled directly in buildExistsSubquery as a coalesced
		// IN (...); this branch is unreachable for nested filters. Kept for
		// total mapping completeness.
		return "="
	}
	return "="
}

// scopeNestedFiltersForCaller decides whether the caller may filter across each
// relation, and — for the ones they may — narrows the EXISTS subquery to the
// rows they are allowed to see. The two halves are one function on purpose: the
// refusal it no longer issues is replaced by the predicate it attaches, so a
// path that skips this call gets neither, and the count oracle it exists to
// close is wide open.
//
// `?author.email=jane@example.com` does not return the related row, so it is
// not a disclosure the way `?include=author` was — but it is an oracle: the
// parent's row count changes with the guessed value, so a caller can confirm
// any value in a column the entity's own route refuses to serve. Filtering
// across a relation is a use of that relation's data, so the same posture
// governs it.
//
// Two things are decided here.
//
// May the caller read the target at all? CanReadScoped, not the narrower
// canReadEntityGate: the include path can afford the narrow gate because it
// scopes rows per node, and this one now does the same — but CanReadScoped is
// still the correct predicate for "may this caller read this entity", and it is
// what answers the question when the target is not row-scoped at all.
//
// Which of the target's rows may they count? Every one the target's own list
// route would serve them, and no others. The predicates come from
// eagerScopeFilters — the same builder the include and eager loaders use — so
// the three surfaces cannot drift into three different answers. It fails closed
// by construction: a caller with no owner in context is narrowed to the empty
// value, which matches no real row, so a guessed value confirms nothing. A
// caller holding an explicit cross-owner or cross-tenant grant gets no
// predicate for that axis, because they already read every row of the target
// through its own routes and narrowing would remove a capability without
// protecting anything.
//
// This replaces a blanket refusal of every owner-scoped or multi-tenant target,
// which closed the oracle but also refused an owner filtering their OWN rows —
// the ordinary case, and the one `?rel.field=` is most useful for. The axes
// stay independent: a cross-owner grant narrows nothing on the tenant axis and
// vice versa, because eagerScopeFilters emits them separately.
func (ch *CrudHandler) scopeNestedFiltersForCaller(ctx context.Context, filters []nestedFilter) error {
	return ch.scopeNestedFilters(ctx, filters, true)
}

// scopeNestedFilters narrows every filter to the caller. checkPosture asks the
// target's Exposure whether this caller may read it at all, which is the
// baseline SESSION gate and therefore an HTTP-only question, exactly as it is
// on the include path (include.go): in-process callers are server-side code
// running with no session by construction, so asking it there refuses every
// nested filter a background job or a typed repo makes and protects nothing.
//
// The SCOPE predicates below are the other half and they are unconditional.
// They are data scoping — owner, tenant, read scope — and data scoping applies
// to both surfaces, because they are what stops the EXISTS clause counting
// rows the caller may not see. Splitting the two is the whole fix: the leak
// was never the missing posture check, it was the missing predicates.
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
				// Unresolvable target: refuse rather than filter against a table
				// nobody vouched for, matching the include path's stance.
				return &includeForbiddenError{Entity: filters[i].Relation.Entity}
			}
			probe := &CrudHandler{Entity: target, DB: ch.DB, Registry: ch.Registry}
			if checkPosture && !probe.CanReadScoped(ctx) {
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
			probe := &CrudHandler{Entity: target, DB: ch.DB, Registry: ch.Registry}
			if checkPosture && !probe.CanReadScoped(ctx) {
				return &includeForbiddenError{Entity: target.GetName()}
			}
			var scopes []filter.ParsedFilter
			scopes = append(scopes, eagerScopeFilters(ctx, target)...)
			scopes = append(scopes, readScopeFilters(ctx, target)...)
			filters[i].Hops[h].Scopes = scopes
			if h == 0 {
				filters[i].scopes = scopes
			}
			parent = target
		}
	}
	return nil
}
func resolvedTable(target *entity.Entity, rel entity.Relation) string {
	if target != nil && target.GetTable() != "" {
		return target.GetTable()
	}
	return rel.Entity
}

// scopeNestedFiltersInProcess applies the caller-aware narrowing to a nested
// filter resolved through the in-process API. It narrows ALWAYS.
//
// It used to narrow only when the context carried realRequestKey, on the
// theory that an in-process call is server code acting on its own authority
// and a call made while serving a request is not. The theory was fine; the
// mechanism could not implement it. withRealRequest is unexported and is set
// at exactly three sites, all of them wrapping the context crud hands to
// applyIncludeTree inside its own HTTP handlers — and applyIncludeTree never
// reaches ListAll or CountAll. So no caller outside this package could ever
// set the marker, the narrowing branch was unreachable in every production
// shape, and the only thing proving it worked was a test that could call the
// unexported setter because it lives in-package.
//
// The default is now the safe one. A host handler that forwards a
// caller-influenced NestedFilter into ListAll or CountAll gets the same
// narrowing the HTTP path applies, so the EXISTS clause cannot count rows the
// target's own posture hides — the count oracle, reached one layer down.
//
// Server-authority code that genuinely means "read across every owner" says so
// with the escape that already exists for exactly this and that every other
// read path honours: owner.AllowCrossOwner and tenant.AllowCrossTenant. Both
// are documented as an escape for the in-process Go surface, both are
// unreachable from an HTTP route, and both are explicit at the call site,
// which an absent context marker never was.
func (ch *CrudHandler) scopeNestedFiltersInProcess(ctx context.Context, filters []nestedFilter) error {
	if len(filters) == 0 {
		return nil
	}
	return ch.scopeNestedFilters(ctx, filters, false)
}
