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
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

const (
	// maxIncludeDepth bounds how many relation hops a single ?include= path
	// may take. Every hop multiplies the loaded row set by that relation's
	// fan-out, so an unbounded path is exponential in a request the client
	// pays ~8 bytes per level for: `include=up.down.up.down.up.down` on a
	// self-referencing entity turned 23 request bytes into 13.7 MB, and two
	// more levels exhausted memory. Four hops covers every include the docs
	// show and every one the app generator emits.
	maxIncludeDepth = 4

	// maxIncludePaths bounds how many comma-separated ?include= paths
	// one request may carry. Depth and the row budget do not bound
	// BREADTH of the node set: every mention carrying a DISTINCT
	// scoped-filter set gets its own IncludeNode and its own SELECT,
	// and a zero-row node still costs a query (a network round trip on
	// Postgres), so 512 such mentions answered 200 after ~514 queries
	// with the 20k-row budget never tripping. Real usage is a handful
	// of relations; 32 paths × 4 hops still leaves generous room.
	maxIncludePaths = 32

	// maxIncludeRows bounds the total number of rows an eager-load may scan
	// across every node of one request's include forest, and, separately,
	// the number of row references the assembled response tree may carry.
	// The depth cap alone does not bound breadth (`include=a.b,c.d,e.f,…`
	// stays within depth while multiplying node count), and neither cap
	// bounds a relation whose fan-out is large at every level. Counting
	// references matters as much as counting scans: N parents sharing one
	// eager-loaded row scan it once but serialise it N times, so a bound on
	// scans alone still lets the response multiply. This is the backstop
	// that makes the worst case a 400 instead of an OOM.
	maxIncludeRows = 20000
)

// errIncludeBudget marks an include that exceeded maxIncludeRows. It is a
// client-caused bound, so the HTTP surfaces render it as 400 (narrow the
// include or paginate) rather than as a 500.
var errIncludeBudget = i18n.NewError(reasonIncludeBudget)

// includeBudget is the per-request row allowance, spent by every eager-load
// as it scans. Threaded rather than stored on CrudHandler because a handler
// is shared across concurrent requests.
type includeBudget struct{ remaining int }

func newIncludeBudget() *includeBudget { return &includeBudget{remaining: maxIncludeRows} }

// spend charges n rows. Returns errIncludeBudget once the request has run
// through its allowance so the caller can stop mid-scan or mid-walk rather
// than materialising the rest.
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

// convertedSubtree is one memoised deep-conversion: the JSON-cased output
// and the number of row references the subtree serialises to. The count is
// what lets a shared subtree be converted once but charged to the budget
// every time it is referenced, the conversion is shared, the bytes on the
// wire are not.
type convertedSubtree struct {
	out   map[string]any
	nodes int
}

// IncludeNode represents one segment of a (possibly nested) ?include=
// expression. The tree is rooted at the request's entity; each node carries
// the relation taken to reach it and any deeper child includes.
//
// Filters narrows the eager-load to rows on the target that match every
// scoped predicate, e.g. include=comments(status=published) attaches only
// published comments. Operator suffixes work the same way they do for
// top-level filters (filter.FilterSuffixes, one shared table).
type IncludeNode struct {
	Name     string                // segment name (matches the relation's Name)
	Relation entity.Relation       // relation declared on the parent entity
	Target   *entity.Entity        // the entity Reached by following Relation
	Filters  []filter.ParsedFilter // scoped filters applied during eager-load
	// ReadScopes carries the TARGET's Exposure.ReadScope predicates, set by
	// applyRelatedReadScope. It is deliberately a SEPARATE slice from
	// Filters: Filters are caller-controlled and rendered with IN-run
	// coalescing, so merging a scope `status_in=[published]` with a caller's
	// `include=rel(status_in=draft)` would coalesce the two runs into one
	// IN list, OR where the declaration demands AND, and serve the drafts
	// the scope exists to hide.
	ReadScopes []filter.ParsedFilter
	// HookScopes carries the WHERE clauses the TARGET's own BeforeList
	// (and, for a to-one relation, BeforeGet) hooks appended, set by
	// applyChildBeforeHooks. They render as one key-IN subquery on the
	// target table, so a clause written against GET /<target> reads the
	// same columns here.
	HookScopes []hook.WhereClause
	// Pivot is the registered entity owning a many_to_many's Through
	// table, nil when the migrator created it. PivotScopes carries its
	// owner, tenant and read-scope predicates (applyPivotScope): a link
	// row the join entity's own route hides links nothing.
	Pivot       *entity.Entity
	PivotScopes []filter.ParsedFilter
	// PivotHookScopes carries the clauses the join entity's own BeforeList
	// appended (applyChildBeforeHooks), rendered as one key-IN subquery on
	// the Through table.
	PivotHookScopes []hook.WhereClause
	Children        []*IncludeNode // deeper includes, e.g. for "author.profile" the "profile" child of "author"
	// scopedNodes, on the first node parsed for a relation, maps each
	// scoped-filter set mentioned for it (the empty set included) to the
	// node that loads it: this node or a sibling spawned for a distinct
	// set. A repeat of a set continues on that node, so its children
	// attach there and no second load of the same rows runs.
	scopedNodes map[string]*IncludeNode
	childMap    map[string]*IncludeNode
}

// scopedClauseKey is the canonical spelling of a scoped-filter set:
// field/op/value per filter, so whitespace variations of one clause
// still match. parsed must come from parseScopedFilters. Field and op are
// identifiers; the value is quoted, so no value can forge a separator.
func scopedClauseKey(parsed []filter.ParsedFilter) string {
	var b strings.Builder
	for _, f := range parsed {
		b.WriteString(f.Field)
		b.WriteByte(' ')
		b.WriteString(string(f.Op))
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(f.Value))
		// The term an IN value came from: a_in=1|2,a[in]=2 and
		// a_in=1,a[in]=2|2 spell one value run but are different
		// predicates (two lists ANDed, split at different places).
		// Only IN needs it: a scalar term is one predicate whichever
		// spelling (column or wire alias) named it.
		if f.Op == filter.OpIn {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(f.Key()))
		}
		b.WriteByte(';')
	}
	return b.String()
}

// parseIncludeTree splits comma-separated dotted include paths and resolves
// each segment against the registry. Returns the roots of the include forest.
//
// Example: "author.profile, comments" against a posts entity yields two
// roots: author (with profile as a child) and comments (no children).
func parseIncludeTree(r *http.Request, ent *entity.Entity, registry entity.Registry) ([]*IncludeNode, error) {
	return parseIncludeTreeQ(r.URL.Query(), ent, registry)
}

// parseIncludeTreeQ is the no-reparse variant. The List handler threads
// its single url.Values through; this avoids r.URL.Query() re-parsing
// RawQuery on every helper call.
func parseIncludeTreeQ(q url.Values, ent *entity.Entity, registry entity.Registry) ([]*IncludeNode, error) {
	raw := strings.TrimSpace(q.Get("include"))
	if raw == "" {
		return nil, nil
	}
	if registry == nil {
		// No registry means no schema for the target entity, and every
		// eager-load guard is keyed off that schema: the Hidden-column
		// scrub, owner scope, tenant scope, the soft-delete filter and the
		// scoped-filter field allow-list. Serving the include anyway is a
		// `SELECT *` of a table nobody vouched for, that is how a related
		// auth table's password_hash reaches a caller. Refuse instead.
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
			// Resolve the target entity. Registration is required for EVERY
			// segment, leaf included. The leaf used to be exempt, "EagerLoad
			// just hits the relation's target table by name", but that
			// dropped the entire guard set that hangs off Target: the
			// Hidden-column scrub (eager_filtered.go), owner scope, tenant
			// scope, the soft-delete filter and the scoped-filter field
			// allow-list. A relation pointed at a self-migrated table
			// (auth_users) then returned every column of any row the FK
			// named. Unresolvable target = refuse, at every depth.
			// Resolve against the SOURCE entity's version. registry.Get prefers
			// the unversioned registration, so a request under /api/v1 whose
			// relation targets a name that also exists unversioned would adopt
			// the unversioned entity's Hidden set and scopes, disclosing
			// columns v1 hides and returning rows v1's owner/tenant/soft-delete
			// scopes exclude. Ambiguity is an error, never a silent pick.
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
				// Scoped filters are validated against the TARGET entity's
				// fields, the allow-list that keeps `include=rel(col=v)`
				// from naming an arbitrary column. Target is always resolved
				// now, so this list is never empty.
				parsed, err = parseScopedFilters(filterClause, entity.ValueFields(registry, target), path)
				if err != nil {
					return nil, err
				}
			}
			// Each filter set mentioned for this relation, the empty one
			// included, has its own node. ANDing a second set into the
			// first node composed a predicate no row satisfies when the
			// sets disagree, and merging an unfiltered mention into a
			// filtered node dropped every row the filter excludes; either
			// way the relation silently came back short. Distinct sets
			// load as sibling nodes, and the loaders union them per
			// relation name. A repeat of a set continues on the node that
			// holds it, wherever that node sits among the siblings.
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
// Returns the unparenthesized name with empty filter if no parens are
// present. Treats unbalanced parens as a parse error by returning the raw
// segment with an empty filter, the relation lookup will then fail with
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

// splitIncludeDelim splits s on sep, but only at paren depth 0 so filter
// clauses keep their parenthesised content intact:
// "comments(status=draft,body_like=x),author" split on ',' must yield
// ["comments(status=draft,body_like=x)", "author"], not four broken
// fragments; an include path split on '.' keeps dotted filter values
// whole. It replaces the former splitIncludeList and splitIncludePath,
// verbatim copies of each other differing only in the separator.
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

// parseScopedFilters parses "status=published,body_like=%foo%" into a slice
// of ParsedFilter, honouring the same suffix operators (_gt/_gte/_lt/_lte/
// _like/_in) that top-level filters do. fields can be nil, in that case
// every field name is accepted at parse time.
func parseScopedFilters(raw string, fields []schema.Field, pathForErrors string) ([]filter.ParsedFilter, error) {
	knownField := map[string]bool{}
	// colType records each visible column's schema type so the built
	// ParsedFilters carry the same coercion ParseFiltersValues applies
	// (a raw "true" binds TEXT against SQLite's INTEGER Bool storage).
	colType := map[string]schema.FieldType{}
	var noQueryField map[string]bool
	// wireAlias maps a field's wire key to its column, so a scoped filter may
	// use the name clients are actually told, ?include=comments(content=x)
	// must work for the same reason ?content=x does at the top level.
	// Populated only for non-Hidden fields, so a hidden column stays
	// unreachable under its alias as well as its column name.
	wireAlias := map[string]string{}
	for _, f := range fields {
		// A Hidden column is treated as NOT declared, the identical
		// "not on target entity" error a nonexistent field gets. The
		// eager loader scrubs Hidden columns from the OUTPUT, but a
		// scoped filter on one would still reach the WHERE clause, and
		// the related row's presence/absence in the response leaks
		// whether the value matched, the same value-disclosure oracle
		// the top-level and nested filter paths close (see
		// nested_filter.go), one relation hop away. NoQuery columns are
		// blocked for the same reason, but tracked separately so the error
		// can name them, they are visible in the response, so folding them
		// into "not on target entity" only sends a developer hunting for a
		// typo in a column they can plainly see.
		if f.NoQuery && !f.Hidden {
			if noQueryField == nil {
				noQueryField = map[string]bool{}
			}
			noQueryField[f.Name] = true
			// Under its wire key too. The alias is what clients are told to
			// send, so a refusal that only knows the column name would let
			// ?include=comments(writer=x) through as "not on target entity"
			// at best, and, if the alias ever reached knownField, past the
			// guard entirely.
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
		// Split the key the way the top-level parser does (filter.ReadKey):
		// name[op] names the whole field before the brackets, a suffix
		// counts only when the name before it is a declared field, a key
		// that is itself a field (opt_in) is that field under equality, and
		// a key that is both (check beside check_in) is refused. With fields
		// nil every name is accepted, so the first suffix cut stands, as it
		// always has: a key cannot read two ways against no field list.
		named := func(n string) bool { return knownField[n] || noQueryField[n] }
		if fields == nil {
			// Any cut name or bracket name is a field; the whole key is
			// never asked about, so a suffixed key cuts and a plain one
			// falls back to itself under equality.
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
		// Resolve the wire key to its column AFTER the allow-list check:
		// ParsedFilter.Field reaches the WHERE clause, so emitting the alias
		// would name a column that does not exist.
		if col, ok := wireAlias[field]; ok {
			field = col
		}
		if fields != nil {
			if err := filter.CheckOpType(sent, op, colType[field]); err != nil {
				return nil, i18n.Wrap(reasonScopedFilter, err, map[string]any{"include": strconv.Quote(pathForErrors)})
			}
		}
		if op == filter.OpIn {
			// Count separators before splitting, so an oversized list is
			// rejected without materializing it. Same shape as the top-level
			// ?field_in= path (filter.SplitINValuesBounded).
			if n := strings.Count(value, "|") + 1; n > maxScopedINEntries {
				return nil, i18n.NewError(reasonScopedInListOverCap, map[string]any{"include": strconv.Quote(pathForErrors), "field": strconv.Quote(field), "count": n, "max": maxScopedINEntries})
			}
			vals := strings.SplitSeq(value, "|")
			for v := range vals {
				pf, err := scopedTyped(filter.ParsedFilter{Field: sent, Op: filter.OpIn, Value: v}, field, colType[field], pathForErrors)
				if err != nil {
					return nil, err
				}
				// Tag each value with its term, so author_id_in=1|2 and
				// its alias writer_in=3, or check_in=a and check[in]=b,
				// render as two ANDed IN lists.
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

// scopedTyped checks a scoped filter's value against its column type
// (the error names the field the caller sent, pf.Field) and returns the
// filter on column col with its typed value, so a mistyped value inside
// ?include=rel(...) answers 400 on both dialects.
func scopedTyped(pf filter.ParsedFilter, col string, t schema.FieldType, pathForErrors string) (filter.ParsedFilter, error) {
	typed, err := pf.Typed(t)
	if err != nil {
		return filter.ParsedFilter{}, i18n.Wrap(reasonScopedFilter, err, map[string]any{"include": strconv.Quote(pathForErrors)})
	}
	typed.Field = col
	return typed, nil
}

// maxScopedINEntries caps the size of a single `field_in=a|b|c|…` list
// passed through an include's scoped filter. Without a cap, a single
// request can blow up a JOIN with thousands of bind parameters, a
// cheap DoS even before SQL parameter limits start refusing the query.
const maxScopedINEntries = 256

// relationByName looks up a Relation a caller may name on an entity. A
// relation behind a Hidden field (entity.HidesRelation) is not found, the
// answer a name that is no relation gets: its include would put the
// related record, and the hidden key, in the response, and a nested
// filter across it would test the record's values.
func relationByName(ent *entity.Entity, name string) (entity.Relation, bool) {
	for _, rel := range ent.Config.Relations {
		if rel.Name == name && !ent.HidesRelation(rel) {
			return rel, true
		}
	}
	return entity.Relation{}, false
}

// includeNearMiss names the relation an unknown include segment most
// likely meant: the segment plus "_id" (a relation named after its
// foreign key, ?include=issue for issue_id), the foreign key itself, or
// the target entity in either number (?include=users for author). Only
// a guess this close gets an answer; the error never lists the entity's
// relations, so an unrelated probe learns nothing about the schema, and
// it never names a relation relationByName would not find.
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

// writeIncludeError renders an eager-load failure. A blown row budget is
// the client's doing, it asked for more related rows than one response may
// carry, so it renders as 400 with an actionable message. Everything else
// is a server fault and stays an opaque 500 with the detail in the log.
func writeIncludeError(w http.ResponseWriter, r *http.Request, surface string, err error) {
	if errors.Is(err, errIncludeBudget) {
		writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
		return
	}
	if forbidden, ok := errors.AsType[*includeForbiddenError](err); ok {
		// The refusal the target entity's own list route would give for an
		// RBAC or session failure. It is not byte-for-byte parity in every
		// posture: an owner-scoped target skips the session check here (see
		// canReadEntityGate), so an anonymous include answers 200 with an
		// empty relation where the target's own route answers 401. Nothing
		// leaks either way, the owner scope matches no row, but the two
		// status codes differ.
		writeErrJSON(w, r, http.StatusForbidden, forbidden, i18n.Phrase{})
		return
	}
	if refused, ok := errors.AsType[*childHookError](err); ok {
		// The child's own route answers its Before hook's error as 400 with
		// the hook's wording or phrase (List and Get in crud.go), so the
		// same refusal reached through the relation answers the same.
		writeErrJSON(w, r, http.StatusBadRequest, refused.err, i18n.Phrase{})
		return
	}
	logf("crud: %s include failed: %v", surface, err)
	writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
}

// applyIncludeTree eager-loads the include forest onto the parent rows. Top-
// level rows are JSON-cased (as they came out of scanRows); nested rows are
// kept in raw DB casing during recursion so further EagerLoad calls can find
// foreign-key columns. The very last step deep-converts everything attached
// under the include keys to JSON case.
//
// Before dispatching each node we inject an owner-scope filter when the
// related entity has an OwnerField configured. Without this, a request
// like `/posts?include=comments` would honour owner-scope on /posts (so
// only alice's posts come back) but pull EVERY comment whose post_id
// matches, including bob's comments on alice's post. The scope inherits
// from the request's owner extractor; if no owner is in context, the
// scope predicate becomes `owner_field = ""` which matches no real row.
func (ch *CrudHandler) applyIncludeTree(ctx context.Context, rows []map[string]any, nodes []*IncludeNode) error {
	if len(nodes) == 0 {
		return nil
	}
	// An include is a read of ANOTHER entity's rows, so that entity's posture
	// governs it. Every other guard hanging off Target, the Hidden scrub,
	// owner scope, tenant scope, soft delete, was already applied here, but
	// Exposure.Access was not: `GET /api/posts?include=author` returned whole
	// rows of an entity whose own route answers 403, and
	// `?include=comments.author` dumped the entire table in one request.
	//
	// Enforced only for a request arriving over HTTP. The in-process callers
	// of this function, the programmatic API and typed repos, are trusted
	// server-side code running with no session by design, exactly as the
	// baseline session gate is an HTTP-only concern (requireAuthenticated)
	// while owner and tenant scoping, which are data scoping, apply to both.
	// Checked before any load and at every depth, so a nested segment cannot
	// reach past a readable parent.
	if _, overHTTP := ctx.Value(realRequestKey{}).(*http.Request); overHTTP {
		if err := ch.checkIncludeReadable(ctx, nodes); err != nil {
			return err
		}
	}
	// Only AFTER the posture check. Returning early on an empty parent made the
	// answer depend on whether the table happened to have rows: the same
	// request answered 200 while empty and 403 once a row existed, which is a
	// table-emptiness oracle and disagreed with the nested-filter check, which
	// refuses before it queries anything.
	if len(rows) == 0 {
		return nil
	}
	// Rows are named by record id, so a composite-key parent loads the
	// relations its key allows (see entity.CheckRelationKeys).
	parentKey := ch.keyCols()
	ids := collectRowKeys(rows, parentKey, ch.convertKey)

	// Build the result map shape once; loadIncludeNode populates it relation
	// by relation so scoped filters can be applied per-node.
	loaded := make(map[string]map[string]any, len(ids))
	for _, id := range ids {
		loaded[id] = make(map[string]any)
	}
	budget := newIncludeBudget()
	for _, node := range nodes {
		applyRelatedOwnerScope(ctx, node)
		applyRelatedTenantScope(ctx, node)
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
		// An included row's schema.JSON columns get the same treatment as
		// the parent's, keyed by raw DB column because the eager loaders
		// case-convert only at the end of the walk. Without this a JSON
		// field is an object on GET /children and a string one relation
		// hop away.
		decodeEntityReadColumns(node.Target, gatherLoadedRows(loaded, node.Relation.Name))
	}
	dedupeRepeatedRelations(nodes, loaded)

	if err := ch.recurseIncludeChildren(ctx, nodes, loaded, budget); err != nil {
		return err
	}

	// Attach to parent rows + deep-convert keys (top-level outer key uses
	// JSON case, the entire nested subtree gets the same treatment). The
	// converted-map memo is per-response: a BelongsTo attaches the very same
	// target map to every parent that names it, so without sharing the
	// conversion the subtree is re-copied once per parent at every level.
	//
	// The memo is keyed by map alone, so every map must convert under one
	// tree: duplicate mentions of a relation share their rows, and so share
	// one tree merged from all of them.
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

	// Child read hooks run last, on the converted maps now attached to the
	// parent rows. See applyChildReadHooks for why the ordering matters.
	return ch.applyChildReadHooks(ctx, nodes, rows)
}

// recurseLoadOnRawRows operates on rows that are still in raw DB casing, the
// nested data EagerLoad produced. It re-runs EagerLoad with each child's
// target relation against those rows, then recurses again.
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
		applyRelatedOwnerScope(ctx, node)
		applyRelatedTenantScope(ctx, node)
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
	// Attach onto the raw rows under the raw relation name (no case conversion
	// here, that happens once at the outermost merge).
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

// dedupeRepeatedRelations keeps each child row once per parent for a
// relation the include names more than once. Each mention loads with its
// own filters and appends to the same slot, so a row matching two of them
// arrived twice. Rows are told apart by the target's key, still in raw DB
// casing here; a row with no readable key is kept.
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
			// A HasOne or BelongsTo slot holds one row, which the second
			// load replaced rather than appended to: only a list repeats.
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

// recurseIncludeChildren walks the include nodes that carry children of
// their own and recurses the freshly loaded rows into them. It is the
// single loop behind the verbatim copies that used to close out
// applyIncludeTree and recurseLoadOnRawRows.
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

// gatherLoadedRows walks loaded[parentID][relName] entries and returns the
// flat list of nested rows, regardless of HasOne/HasMany/etc. shape.
//
// Rows are deduplicated by map identity. A BelongsTo attaches the SAME
// target map to every parent whose FK names it, so N parents pointing at
// one row used to yield that row N times, the next level then re-loaded
// and re-attached it N times, which is the per-level multiplication that
// made a six-hop include exponential.
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

// collectRowKeys returns each row's record id, read from the key columns
// cols under the names names gives them (see entity.RowKey). Rows without
// a usable id are skipped.
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

// rawRowKey is a raw database row's record id. Postgres reports a
// mixed-case column under its folded name, so a key column is matched
// ignoring case when its exact name is absent.
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

// convTree is the wire-key converter for one include hop's rows, and by
// raw relation name the trees for the hops nested under it.
//
// documents names the hop's schema.JSON columns. Their values are stored
// documents, data and not wire keys, so their keys are kept as stored, the
// way a direct read keeps them. Converted, a stored {"key_name": 1} read
// back as keyName through ?include= and the next save of that value
// rewrote the document.
type convTree struct {
	conv      func(string) string
	children  map[string]*convTree
	documents map[string]bool
}

// hopTree is the converter tree of one hop over target's rows, without
// the hops nested under it. A nil target converts with the handler's own
// keys and treats no column as a document.
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

// includeConvTrees builds the converter tree of each relation nodes
// include, by relation name. Each hop's rows belong to that hop's
// target, so its keys convert with the target's wire map: posts aliasing
// body_text->"summary" must not rename comments' body_text, which
// declares its own alias "content", and neither may rename the
// enrollments included under a comment's student. A relation mentioned
// twice with distinct filters is two nodes over one set of rows; their
// trees merge, at every depth.
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

// mergeConvTree adds src's nested trees to dst, merging those both hold.
func mergeConvTree(dst, src *convTree) {
	for k, v := range src.children {
		if d, ok := dst.children[k]; ok {
			mergeConvTree(d, v)
			continue
		}
		dst.children[k] = v
	}
}

// formatRelationValueDeep is like formatRelationValue but recursively
// converts every nested map's keys to JSON case, including any subtrees
// previously attached during recurseLoadOnRawRows. tree converts each
// hop with its own target's wire map; nil converts rel's target only.
// Resolution is version-aware (entity.ResolveTarget) for the same reason
// the include tree is: a v1 request must not adopt an unversioned
// declaration's field naming.
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

// deepConvertMap walks a value tree, converting every map key (including
// keys inside nested maps and slices) with tree's converter, and the
// value under a nested relation's key with that relation's tree. A nil
// tree converts with ch.convertKey. Non-map values pass through
// unchanged.
//
// converted memoises by source-map identity, so a subtree reached from many
// parents is converted once and the result shared. Both the aliasing and the
// memo matter: eager-loading attaches one BelongsTo target map to every
// parent that names it, and copying that subtree per parent at every level
// is what turned a 23-byte include into 13.7 MB. Recording the output map
// before filling it also makes a self-referential row terminate instead of
// recursing forever.
// Returns the converted value plus the number of row references it
// serialises to, so callers can charge a re-referenced subtree its full
// weight without re-walking it.
func (ch *CrudHandler) deepConvertMap(v any, tree *convTree, converted map[uintptr]*convertedSubtree, budget *includeBudget) (any, int, error) {
	if tree == nil || tree.conv == nil {
		tree = &convTree{conv: ch.convertKey}
	}
	switch x := v.(type) {
	case map[string]any:
		id := reflect.ValueOf(x).Pointer()
		if prev, ok := converted[id]; ok {
			// Already converted for another parent: reuse the output, but
			// charge the whole subtree again, this reference costs the
			// response the same bytes the first one did.
			if err := budget.spend(prev.nodes); err != nil {
				return nil, 0, err
			}
			return prev.out, prev.nodes, nil
		}
		if err := budget.spend(1); err != nil {
			return nil, 0, err
		}
		out := make(map[string]any, len(x))
		// Recorded before the walk so a self-referential row terminates
		// instead of recursing forever.
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

// applyRelatedOwnerScope prepends an owner-scope filter to the include
// node when the related entity has an OwnerField configured. This is
// the cross-table half of owner scoping, without it, ?include=comments
// on /posts would honour owner scope on posts but not on comments,
// letting alice's response include bob's comments on a post she owns.
//
// If no owner extractor is wired or the request has no owner in
// context, the predicate becomes `owner_field = ""` which matches no
// real row, fail-safe.
func applyRelatedOwnerScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	ownerField := node.Target.Config.Scope.OwnerField
	if ownerField == "" {
		return
	}
	// Cross-owner callers see every row on the routes (ApplyOwnerScope and its
	// update/delete siblings all branch on this pair). Without the same branch
	// here, an include for that posture silently returned a near-empty relation
	// rather than the rows the caller is entitled to, the identical defect
	// applyRelatedTenantScope had for tenants, fixed alongside this one.
	//
	// The grant is read from the TARGET entity, because these are the target's
	// rows: a probe handler is the same shape the include gate uses.
	if owner.IsCrossOwner(ctx) {
		return
	}
	if probe := (&CrudHandler{Entity: node.Target}); probe.crossOwnerReadGranted(ctx) {
		return
	}
	// Always AND the context-derived owner predicate, even when the node
	// already carries a filter on the owner field. On the HTTP path
	// node.Filters comes from the attacker-controlled
	// `include=rel(owner_field=val)` query string; treating that as an
	// opt-out would let a forged `user_id=bob` disable cross-table owner
	// scoping (IDOR). Intersecting it with the real owner instead means a
	// forged value matches nothing, fail-closed. A legitimate caller who
	// filters on their own id gets a redundant-but-harmless predicate.
	var val string
	if id, ok := owner.Get(ctx); ok && id != nil {
		val = fmt.Sprintf("%v", id)
	}
	node.Filters = append([]filter.ParsedFilter{{
		Field: ownerField,
		Op:    filter.OpEq,
		Value: val,
	}}, node.Filters...)
}

// applyPivotScope sets the predicates a many_to_many's join entity puts
// on its own rows: owner, tenant and read scope, the ones its list route
// applies. A link the caller cannot see on the join entity does not link
// the two sides for them either.
func applyPivotScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Pivot == nil {
		return
	}
	node.PivotScopes = pivotScopes(ctx, node.Pivot)
}

// applyRelatedTenantScope is the tenant analog of applyRelatedOwnerScope.
// When the included child entity is MultiTenant, it prepends a
// `tenant_id = <ctx tenant>` predicate so an ?include= can't reach across
// tenants, without it, `/posts?include=comments` would scope posts to the
// caller's tenant but pull EVERY comment whose post_id matches, including
// other tenants' rows on a shared post id.
//
// If the request has no tenant in context, the predicate becomes
// `tenant_id = ""` which matches no real row, fail-closed, exactly like
// the owner version. As with the owner scope, this is always ANDed (never
// an opt-out) so an attacker-supplied scoped filter on the tenant column
// can't disable it.
func applyRelatedTenantScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	if !node.Target.Config.Scope.MultiTenant {
		return
	}
	// Cross-tenant callers see every tenant's rows on the routes
	// (RequireTenant and ApplyTenantScope both branch on this). Without the
	// same branch here, an include for that posture silently returned an empty
	// relation rather than the rows the caller is entitled to, fail-closed,
	// but contradicting the documented contract.
	if tenant.IsCrossTenant(ctx) {
		return
	}
	node.Filters = append([]filter.ParsedFilter{{
		Field: node.Target.Config.TenantColumn(),
		Op:    filter.OpEq,
		Value: tenant.GetTenantID(ctx),
	}}, node.Filters...)
}

// applyRelatedReadScope attaches the TARGET's Exposure.ReadScope predicates
// to the include node, the read-scope sibling of applyRelatedOwnerScope /
// applyRelatedTenantScope. Without it, `?include=posts` on another entity
// serves the drafts the posts entity's own route hides; the include path is
// exactly where cross-table leaks live.
//
// The predicates land in node.ReadScopes, NOT node.Filters: Filters render
// with IN-run coalescing against caller-controlled scoped filters, and a
// coalesced run would OR the scope's values with the caller's instead of
// ANDing them (see IncludeNode.ReadScopes). An unrestricted caller (the
// ReadScope's own rule) gets no predicates at all, and an entity without a
// ReadScope is untouched.
func applyRelatedReadScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	node.ReadScopes = readScopeFilters(ctx, node.Target)
}

// hopTreeFor returns the converter tree for the entity a relation points
// at, so an included row is renamed by ITS OWN schema rather than the
// parent handler's.
//
// Without this, deepConvertMap applied the parent's wire map at every depth:
// posts aliasing body_text->"summary" renamed comments' body_text to
// "summary" too, ignoring comments' own alias, and a parent with no matching
// column fell through to plain case conversion, so the included entity's
// declared wire contract was never honoured at all.
//
// Resolution is version-aware for the same reason the include tree is: a v1
// request must not adopt an unversioned declaration's field naming. On any
// resolution failure this falls back to the handler's own converter, which is
// the pre-existing behaviour, the include tree has already refused
// unresolvable targets by this point, so this path is defensive only.
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
	// Build the target's wire map once per relation, not per row.
	wire := make(map[string]string, len(target.GetFields()))
	for _, f := range target.GetFields() {
		if f.WireName != "" {
			wire[f.Name] = f.WireName
		}
	}
	if len(wire) == 0 {
		// No aliases on the target: plain case conversion, same as before.
		return ch.convertKeyRaw
	}
	return func(k string) string {
		if w, ok := wire[k]; ok {
			return w
		}
		return ch.convertKeyRaw(k)
	}
}

// includeForbiddenError marks an include whose target entity the caller may not
// read. Carried as a distinct type so the HTTP layer answers 403 naming the
// entity, rather than the generic 500 every other include failure gets.
type includeForbiddenError struct{ Entity string }

func (e *includeForbiddenError) Error() string {
	return reasonIncludeForbidden.Format(map[string]any{"entity": e.Entity})
}

func (e *includeForbiddenError) Phrased() (i18n.Phrase, map[string]any) {
	return reasonIncludeForbidden, map[string]any{"entity": e.Entity}
}

// checkIncludeReadable walks the include forest and refuses any node whose
// target entity fails the same read posture its own list route enforces.
//
// The check is recursive because include paths are: `comments.author` reaches
// `author` through a `comments` the caller legitimately reads, so permitting
// the first segment says nothing about the second.
func (ch *CrudHandler) checkIncludeReadable(ctx context.Context, nodes []*IncludeNode) error {
	for _, node := range nodes {
		if node == nil || node.Target == nil {
			continue
		}
		target := &CrudHandler{Entity: node.Target, DB: ch.DB, Registry: ch.Registry}
		if !target.relationReachable(ctx, "read") || !target.canReadEntityGate(ctx) {
			return &includeForbiddenError{Entity: node.Target.GetName()}
		}
		// A many_to_many reads the join entity's rows too: which members a
		// group has IS the memberships table. A registered pivot answers
		// for itself the way the target does; its owner, tenant and read
		// scope narrow the rows (applyPivotScope), so the gate half is the
		// one to ask here. A migrator-made pivot (nil) has no posture.
		if err := ch.checkPivotReadable(ctx, node.Pivot); err != nil {
			return err
		}
		if err := ch.checkIncludeReadable(ctx, node.Children); err != nil {
			return err
		}
	}
	return nil
}

// checkPivotReadable refuses a many_to_many whose registered join entity the
// caller may not read: its own route answers 403, so the associations it
// holds are not readable through ?include= or ?rel.field= either. nil (a
// migrator-made pivot) passes.
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
