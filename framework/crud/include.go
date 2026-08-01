package crud

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
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
	// show and every one the blueprint emits.
	maxIncludeDepth = 4

	// maxIncludeRows bounds the total number of rows an eager-load may scan
	// across every node of one request's include forest, and — separately —
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
var errIncludeBudget = errors.New("include exceeds the maximum number of related rows; narrow the include or reduce the page size")

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
// every time it is referenced — the conversion is shared, the bytes on the
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
		// `SELECT *` of a table nobody vouched for — that is how a related
		// auth table's password_hash reaches a caller. Refuse instead.
		return nil, fmt.Errorf("include %q requires an entity registry: set CrudHandler.Registry (framework apps do this automatically)", raw)
	}

	var roots []*IncludeNode
	rootMap := map[string]*IncludeNode{}

	for _, path := range splitIncludeList(raw) {
		segments := splitIncludePath(path)
		if len(segments) == 0 {
			continue
		}
		if len(segments) > maxIncludeDepth {
			return nil, fmt.Errorf("include %q is %d relations deep; the maximum is %d", path, len(segments), maxIncludeDepth)
		}

		siblings := &roots
		siblingMap := rootMap
		currentEntity := ent

		for _, segRaw := range segments {
			seg, filterClause := splitSegmentFilter(segRaw)
			rel, ok := relationByName(currentEntity, seg)
			if !ok {
				return nil, fmt.Errorf("unknown include %q (segment %q has no relation on entity %q)", path, seg, currentEntity.GetName())
			}
			// Resolve the target entity. Registration is required for EVERY
			// segment, leaf included. The leaf used to be exempt — "EagerLoad
			// just hits the relation's target table by name" — but that
			// dropped the entire guard set that hangs off Target: the
			// Hidden-column scrub (eager_filtered.go), owner scope, tenant
			// scope, the soft-delete filter and the scoped-filter field
			// allow-list. A relation pointed at a self-migrated table
			// (auth_users) then returned every column of any row the FK
			// named. Unresolvable target = refuse, at every depth.
			// Resolve against the SOURCE entity's version. registry.Get prefers
			// the unversioned registration, so a request under /api/v1 whose
			// relation targets a name that also exists unversioned would adopt
			// the unversioned entity's Hidden set and scopes — disclosing
			// columns v1 hides and returning rows v1's owner/tenant/soft-delete
			// scopes exclude. Ambiguity is an error, never a silent pick.
			target, err := entity.ResolveTarget(registry, ent, rel.Entity)
			if err != nil {
				return nil, fmt.Errorf("include %q: relation %q targets entity %q, which is not registered: %w", path, seg, rel.Entity, err)
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
				// Scoped filters are validated against the TARGET entity's
				// fields — the allow-list that keeps `include=rel(col=v)`
				// from naming an arbitrary column. Target is always resolved
				// now, so this list is never empty.
				parsed, err := parseScopedFilters(filterClause, target.GetFields(), path)
				if err != nil {
					return nil, err
				}
				node.Filters = append(node.Filters, parsed...)
			}
			siblings = &node.Children
			siblingMap = node.childMap
			currentEntity = target
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
	var noQueryField map[string]bool
	// wireAlias maps a field's wire key to its column, so a scoped filter may
	// use the name clients are actually told — ?include=comments(content=x)
	// must work for the same reason ?content=x does at the top level.
	// Populated only for non-Hidden fields, so a hidden column stays
	// unreachable under its alias as well as its column name.
	wireAlias := map[string]string{}
	for _, f := range fields {
		// A Hidden column is treated as NOT declared — the identical
		// "not on target entity" error a nonexistent field gets. The
		// eager loader scrubs Hidden columns from the OUTPUT, but a
		// scoped filter on one would still reach the WHERE clause, and
		// the related row's presence/absence in the response leaks
		// whether the value matched — the same value-disclosure oracle
		// the top-level and nested filter paths close (see
		// nested_filter.go), one relation hop away. NoQuery columns are
		// blocked for the same reason, but tracked separately so the error
		// can name them — they are visible in the response, so folding them
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
			// at best — and, if the alias ever reached knownField, past the
			// guard entirely.
			if f.WireName != "" && f.WireName != f.Name {
				noQueryField[f.WireName] = true
			}
			continue
		}
		if !f.Hidden {
			knownField[f.Name] = true
			if f.WireName != "" && f.WireName != f.Name {
				knownField[f.WireName] = true
				wireAlias[f.WireName] = f.Name
			}
		}
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
	for kv := range strings.SplitSeq(raw, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		before, after, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("include %q: scoped filter %q missing =", pathForErrors, kv)
		}
		key, value := before, after
		field := key
		op := filter.OpEq
		for _, s := range suffixes {
			if before, ok := strings.CutSuffix(key, s.suffix); ok {
				field = before
				op = s.op
				break
			}
		}
		if noQueryField[field] {
			return nil, fmt.Errorf("include %q: scoped field %q cannot be filtered", pathForErrors, field)
		}
		if fields != nil && !knownField[field] {
			return nil, fmt.Errorf("include %q: scoped field %q not on target entity", pathForErrors, field)
		}
		// Resolve the wire key to its column AFTER the allow-list check:
		// ParsedFilter.Field reaches the WHERE clause, so emitting the alias
		// would name a column that does not exist.
		if col, ok := wireAlias[field]; ok {
			field = col
		}
		if op == filter.OpIn {
			vals := strings.Split(value, "|")
			if len(vals) > maxScopedINEntries {
				return nil, fmt.Errorf("include %q: scoped IN list on %q has %d entries (max %d)", pathForErrors, field, len(vals), maxScopedINEntries)
			}
			for _, v := range vals {
				out = append(out, filter.ParsedFilter{Field: field, Op: filter.OpIn, Value: v})
			}
		} else {
			out = append(out, filter.ParsedFilter{Field: field, Op: op, Value: value})
		}
	}
	return out, nil
}

// maxScopedINEntries caps the size of a single `field_in=a|b|c|…` list
// passed through an include's scoped filter. Without a cap, a single
// request can blow up a JOIN with thousands of bind parameters — a
// cheap DoS even before SQL parameter limits start refusing the query.
const maxScopedINEntries = 256

// relationByName looks up a Relation on an entity by name.
func relationByName(ent *entity.Entity, name string) (entity.Relation, bool) {
	for _, rel := range ent.Config.Relations {
		if rel.Name == name {
			return rel, true
		}
	}
	return entity.Relation{}, false
}

// writeIncludeError renders an eager-load failure. A blown row budget is
// the client's doing — it asked for more related rows than one response may
// carry — so it renders as 400 with an actionable message. Everything else
// is a server fault and stays an opaque 500 with the detail in the log.
func writeIncludeError(w http.ResponseWriter, surface string, err error) {
	if errors.Is(err, errIncludeBudget) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("crud: %s include failed: %v", surface, err)
	writeJSONError(w, http.StatusInternalServerError, "internal server error")
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
// matches — including bob's comments on alice's post. The scope inherits
// from the request's owner extractor; if no owner is in context, the
// scope predicate becomes `owner_field = ""` which matches no real row.
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
	budget := newIncludeBudget()
	for _, node := range nodes {
		applyRelatedOwnerScope(ctx, node)
		applyRelatedTenantScope(ctx, node)
		if err := loadIncludeNode(ctx, ch.DB, ch.Entity.GetTable(), ch.PrimaryKey, node, ids, loaded, budget); err != nil {
			if errors.Is(err, errIncludeBudget) {
				return err
			}
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
		if err := ch.recurseLoadOnRawRows(ctx, node.Target, node.Children, nestedRows, budget); err != nil {
			return err
		}
	}

	// Attach to parent rows + deep-convert keys (top-level outer key uses
	// JSON case, the entire nested subtree gets the same treatment). The
	// converted-map memo is per-response: a BelongsTo attaches the very same
	// target map to every parent that names it, so without sharing the
	// conversion the subtree is re-copied once per parent at every level.
	converted := map[uintptr]*convertedSubtree{}
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
			out, err := ch.formatRelationValueDeep(node.Relation, val, present, converted, budget)
			if err != nil {
				return err
			}
			row[outKey] = out
		}
	}

	// Child read hooks run last, on the converted maps now attached to the
	// parent rows — see applyChildReadHooks for why the ordering matters.
	return ch.applyChildReadHooks(ctx, nodes, rows)
}

// recurseLoadOnRawRows operates on rows that are still in raw DB casing — the
// nested data EagerLoad produced. It re-runs EagerLoad with each child's
// target relation against those rows, then recurses again.
func (ch *CrudHandler) recurseLoadOnRawRows(ctx context.Context, target *entity.Entity, children []*IncludeNode, rawRows []map[string]any, budget *includeBudget) error {
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
		applyRelatedOwnerScope(ctx, node)
		applyRelatedTenantScope(ctx, node)
		if err := loadIncludeNode(ctx, ch.DB, target.GetTable(), pk, node, ids, loaded, budget); err != nil {
			if errors.Is(err, errIncludeBudget) {
				return err
			}
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
		if err := ch.recurseLoadOnRawRows(ctx, node.Target, node.Children, nestedRows, budget); err != nil {
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
//
// Rows are deduplicated by map identity. A BelongsTo attaches the SAME
// target map to every parent whose FK names it, so N parents pointing at
// one row used to yield that row N times — the next level then re-loaded
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
func (ch *CrudHandler) formatRelationValueDeep(rel entity.Relation, val any, present bool, converted map[uintptr]*convertedSubtree, budget *includeBudget) (any, error) {
	// Included rows belong to the TARGET entity, so their keys must be
	// converted with the target's wire map — not this handler's. posts
	// aliasing body_text->"summary" must not rename comments' body_text,
	// which declares its own alias "content". Resolution is version-aware
	// (entity.ResolveTarget) for the same reason the include tree is: a v1
	// request must not adopt an unversioned declaration's field naming.
	conv := ch.wireConverterFor(rel.Entity)
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
			c, _, err := ch.deepConvertMap(m, conv, converted, budget)
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
		c, _, err := ch.deepConvertMap(m, conv, converted, budget)
		if err != nil {
			return nil, err
		}
		return c.(map[string]any), nil
	}
}

// deepConvertMap walks a value tree, applying ch.convertKey to every map key
// (including keys inside nested maps and slices). Non-map values pass through
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
func (ch *CrudHandler) deepConvertMap(v any, conv func(string) string, converted map[uintptr]*convertedSubtree, budget *includeBudget) (any, int, error) {
	if conv == nil {
		conv = ch.convertKey
	}
	switch x := v.(type) {
	case map[string]any:
		id := reflect.ValueOf(x).Pointer()
		if prev, ok := converted[id]; ok {
			// Already converted for another parent: reuse the output, but
			// charge the whole subtree again — this reference costs the
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
			c, n, err := ch.deepConvertMap(val, conv, converted, budget)
			if err != nil {
				return nil, 0, err
			}
			total += n
			out[conv(k)] = c
		}
		entry.nodes = total
		return out, total, nil
	case []map[string]any:
		out := make([]map[string]any, len(x))
		total := 0
		for i, m := range x {
			c, n, err := ch.deepConvertMap(m, conv, converted, budget)
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
			c, n, err := ch.deepConvertMap(v, conv, converted, budget)
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
// the cross-table half of owner scoping — without it, ?include=comments
// on /posts would honour owner scope on posts but not on comments,
// letting alice's response include bob's comments on a post she owns.
//
// If no owner extractor is wired or the request has no owner in
// context, the predicate becomes `owner_field = ""` which matches no
// real row — fail-safe.
func applyRelatedOwnerScope(ctx context.Context, node *IncludeNode) {
	if node == nil || node.Target == nil {
		return
	}
	ownerField := node.Target.Config.Scope.OwnerField
	if ownerField == "" {
		return
	}
	// Always AND the context-derived owner predicate, even when the node
	// already carries a filter on the owner field. On the HTTP path
	// node.Filters comes from the attacker-controlled
	// `include=rel(owner_field=val)` query string; treating that as an
	// opt-out would let a forged `user_id=bob` disable cross-table owner
	// scoping (IDOR). Intersecting it with the real owner instead means a
	// forged value matches nothing — fail-closed. A legitimate caller who
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

// applyRelatedTenantScope is the tenant analog of applyRelatedOwnerScope.
// When the included child entity is MultiTenant, it prepends a
// `tenant_id = <ctx tenant>` predicate so an ?include= can't reach across
// tenants — without it, `/posts?include=comments` would scope posts to the
// caller's tenant but pull EVERY comment whose post_id matches, including
// other tenants' rows on a shared post id.
//
// If the request has no tenant in context, the predicate becomes
// `tenant_id = ""` which matches no real row — fail-closed, exactly like
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
	node.Filters = append([]filter.ParsedFilter{{
		Field: node.Target.Config.TenantColumn(),
		Op:    filter.OpEq,
		Value: tenant.GetTenantID(ctx),
	}}, node.Filters...)
}

// wireConverterFor returns the JSON-key converter for the entity a relation
// points at, so an included row is renamed by ITS OWN schema rather than the
// parent handler's.
//
// Without this, deepConvertMap applied the parent's wire map at every depth:
// posts aliasing body_text->"summary" renamed comments' body_text to
// "summary" too, ignoring comments' own alias, and a parent with no matching
// column fell through to plain case conversion — so the included entity's
// declared wire contract was never honoured at all.
//
// Resolution is version-aware for the same reason the include tree is: a v1
// request must not adopt an unversioned declaration's field naming. On any
// resolution failure this falls back to the handler's own converter, which is
// the pre-existing behaviour — the include tree has already refused
// unresolvable targets by this point, so this path is defensive only.
func (ch *CrudHandler) wireConverterFor(targetName string) func(string) string {
	if ch.Registry == nil {
		return ch.convertKey
	}
	target, err := entity.ResolveTarget(ch.Registry, ch.Entity, targetName)
	if err != nil || target == nil {
		return ch.convertKey
	}
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
