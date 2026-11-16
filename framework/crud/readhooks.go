package crud

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/internal/deepcopy"
)

type readHooksKey struct{}

// realRequestKey carries the request being served down to child read hooks.
type realRequestKey struct{}

func withRealRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, realRequestKey{}, r)
}

// requestFrom is the request being served, or a synthetic GET carrying
// hookCtx(ctx).
func (ch *CrudHandler) requestFrom(ctx context.Context) *http.Request {
	if r, ok := ctx.Value(realRequestKey{}).(*http.Request); ok && r != nil {
		return r
	}
	return syntheticRequest(hookCtx(ctx), http.MethodGet, "/")
}

// WithReadHooks returns a context in which ListAll, CountAll, GetOne and
// TypedQuery run the read hooks and masks a GET runs, so they return what
// a GET returns. Use it where rows are displayed, not where they are read
// to be written back:
//
//	rows, err := ch.ListAll(crud.WithReadHooks(ctx), opts)   // a screen
//	row, err := ch.GetOne(ctx, id, nil)                      // an edit form
func WithReadHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, readHooksKey{}, true)
}

func readHooksEnabled(ctx context.Context) bool {
	v, _ := ctx.Value(readHooksKey{}).(bool)
	return v
}

// hookCtx is ctx without the read-hook opt-in, for a hook to run under.
func hookCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, readHooksKey{}, false)
}

// ReadHooksRestrict reports whether the caller's BeforeList or BeforeGet
// hooks, run for a read of record id, append any clause. A hook error
// reports true.
func (ch *CrudHandler) ReadHooksRestrict(ctx context.Context, id string) bool {
	if ch.Hooks == nil {
		return false
	}
	ctx = WithReadHooks(ctx)
	clauses, err := ch.ownBeforeScopes(ctx, ch.requestFrom(ctx), id, true)
	return err != nil || len(clauses) > 0
}

// runBeforeList runs BeforeList for an in-process read under WithReadHooks
// and returns the clauses its hooks appended.
func (ch *CrudHandler) runBeforeList(ctx context.Context, r *http.Request) ([]hook.WhereClause, error) {
	if ch.Hooks == nil || !readHooksEnabled(ctx) {
		return nil, nil
	}
	payload := &hook.ListPayload{Request: hookRequest(r)}
	if err := runHooks(ch.Hooks, hookCtx(ctx), hook.BeforeList, payload); err != nil {
		return nil, fmt.Errorf("before-list hook: %w", err)
	}
	return payload.Where, nil
}

// runBeforeGet is runBeforeList for a by-id read: the BeforeGet chain.
func (ch *CrudHandler) runBeforeGet(ctx context.Context, r *http.Request, id string) ([]hook.WhereClause, error) {
	if ch.Hooks == nil || !readHooksEnabled(ctx) {
		return nil, nil
	}
	payload := &hook.GetPayload{Request: hookRequest(r), ID: id}
	if err := runHooks(ch.Hooks, hookCtx(ctx), hook.BeforeGet, payload); err != nil {
		return nil, fmt.Errorf("before-get hook: %w", err)
	}
	return payload.Where, nil
}

// childBeforeScopes runs target's own BeforeList, and for a to-one relation
// BeforeGet, for a read reaching its rows through rel, and returns the
// clauses they appended.
func (ch *CrudHandler) childBeforeScopes(ctx context.Context, target *entity.Entity, rel entity.Relation) ([]hook.WhereClause, error) {
	return ch.childBeforeScopesFor(ctx, target, rel, "")
}

// childBeforeScopesFor is childBeforeScopes with the id a to-one BeforeGet sees.
func (ch *CrudHandler) childBeforeScopesFor(ctx context.Context, target *entity.Entity, rel entity.Relation, id string) ([]hook.WhereClause, error) {
	if ch.ChildHooks == nil || target == nil {
		return nil, nil
	}
	reg, err := ch.childHookRegistry(target.GetName())
	if err != nil || reg == nil {
		return nil, err
	}
	req := hookRequest(ch.requestFrom(ctx))
	var where []hook.WhereClause
	if len(reg.HooksFor(hook.BeforeList)) > 0 {
		payload := &hook.ListPayload{Request: req}
		if err := runHooks(reg, hookCtx(ctx), hook.BeforeList, payload); err != nil {
			return nil, &childHookError{rel: rel.Name, chain: "before-list", err: err}
		}
		where = append(where, payload.Where...)
	}
	toOne := rel.Type == entity.RelHasOne || rel.Type == entity.RelManyToOne
	if toOne && len(reg.HooksFor(hook.BeforeGet)) > 0 {
		payload := &hook.GetPayload{Request: req, ID: id}
		if err := runHooks(reg, hookCtx(ctx), hook.BeforeGet, payload); err != nil {
			return nil, &childHookError{rel: rel.Name, chain: "before-get", err: err}
		}
		where = append(where, payload.Where...)
	}
	return where, nil
}

// childHookError is a child entity's Before hook refusing a read reached
// through a relation.
type childHookError struct {
	rel, chain string
	err        error
}

func (e *childHookError) Error() string {
	return fmt.Sprintf("relation %s: %s hook: %v", e.rel, e.chain, e.err)
}

func (e *childHookError) Unwrap() error { return e.err }

// childHookRegistry is ChildHooks(name), with a panic returned as an error.
func (ch *CrudHandler) childHookRegistry(name string) (reg *hook.HookRegistry, err error) {
	defer func() {
		if r := recover(); r != nil {
			reg, err = nil, fmt.Errorf("child hooks for %s: %s", name, textsafe.Recovered(r))
		}
	}()
	return ch.ChildHooks(name), nil
}

// applyChildBeforeHooks sets node.HookScopes and node.PivotHookScopes.
func (ch *CrudHandler) applyChildBeforeHooks(ctx context.Context, node *IncludeNode) error {
	if node == nil || !readHooksEnabled(ctx) {
		return nil
	}
	where, err := ch.childBeforeScopes(ctx, node.Target, node.Relation)
	if err != nil {
		return err
	}
	node.HookScopes = where
	pivotWhere, err := ch.childBeforeScopes(ctx, node.Pivot, node.Relation)
	if err != nil {
		return err
	}
	node.PivotHookScopes = pivotWhere
	return nil
}

func pivotHookScope(pivot *entity.Entity, through string, clauses []hook.WhereClause) (string, []any) {
	if pivot == nil || len(clauses) == 0 {
		return "", nil
	}
	return hookScopeSubquery(through, through, pivot.KeyColumns(), clauses)
}

// hookScopeSubquery renders Before-hook clauses as
// "qualifier.key IN (SELECT key FROM table WHERE ...)", numbered from $1.
func hookScopeSubquery(qualifier, table string, key []string, clauses []hook.WhereClause) (string, []any) {
	if len(clauses) == 0 {
		return "", nil
	}
	if !isSafeTable(qualifier) || !isSafeTable(table) || !safeIdentifiers(key) {
		return "1 = 0", nil
	}
	qb := query.Select(key...).From(table)
	for _, c := range clauses {
		qb.Where(c.SQL, c.Args...)
	}
	sub, args := qb.Build()
	lhs := make([]string, len(key))
	for i, k := range key {
		lhs[i] = query.QuoteKeyword(qualifier + "." + k)
	}
	left := lhs[0]
	if len(lhs) > 1 {
		left = "(" + strings.Join(lhs, ", ") + ")"
	}
	return left + " IN (" + sub + ")", args
}

// runAfterGet masks an in-process result and runs AfterGet over it.
func (ch *CrudHandler) runAfterGet(ctx context.Context, r *http.Request, id string, result map[string]any) (map[string]any, error) {
	if !readHooksEnabled(ctx) || result == nil {
		return result, nil
	}
	ch.maskOwn(ctx, result)
	if ch.Hooks == nil {
		return result, nil
	}
	payload := &hook.GetPayload{Request: hookRequest(r), ID: id, Result: result}
	if err := runHooks(ch.Hooks, hookCtx(ctx), hook.AfterGet, payload); err != nil {
		return nil, fmt.Errorf("after-get hook: %w", err)
	}
	return payload.Result, nil
}

// identityOnly reduces a write response to its primary key.
func (ch *CrudHandler) identityOnly(result map[string]any) map[string]any {
	out := map[string]any{}
	for _, c := range ch.keyCols() {
		key := ch.convertKey(c)
		if v, ok := result[key]; ok {
			out[key] = v
		}
	}
	return out
}

// hookRequest is r without the read-hook opt-in on its context.
func hookRequest(r *http.Request) *http.Request {
	if r == nil || !readHooksEnabled(r.Context()) {
		return r
	}
	return r.WithContext(hookCtx(r.Context()))
}

func (ch *CrudHandler) applyAfterList(ctx context.Context, payload *hook.ListPayload) error {
	rowCount := len(payload.Results)
	if err := runHooks(ch.Hooks, ctx, hook.AfterList, payload); err != nil {
		return err
	}
	if len(payload.Results) != rowCount {
		return fmt.Errorf("AfterList changed row count from %d to %d; filter rows in BeforeList", rowCount, len(payload.Results))
	}
	return nil
}

func (ch *CrudHandler) runAfterList(ctx context.Context, r *http.Request, results []map[string]any) ([]map[string]any, error) {
	if !readHooksEnabled(ctx) {
		return results, nil
	}
	ch.maskOwn(ctx, results...)
	if ch.Hooks == nil {
		return results, nil
	}
	payload := &hook.ListPayload{Request: hookRequest(r), Results: results}
	if err := ch.applyAfterList(hookCtx(ctx), payload); err != nil {
		return nil, fmt.Errorf("after-list hook: %w", err)
	}
	return payload.Results, nil
}

// applyChildReadHooks runs each included relation's own masks and read
// hooks over the rows attached to rows, after key conversion, recursively.
func (ch *CrudHandler) applyChildReadHooks(ctx context.Context, nodes []*IncludeNode, rows []map[string]any) error {
	return ch.applyChildReadHooksUnder(ctx, nodes, rows, ch.convertKey)
}

// applyChildReadHooksUnder is applyChildReadHooks over rows whose keys
// convert with keyOf.
func (ch *CrudHandler) applyChildReadHooksUnder(ctx context.Context, nodes []*IncludeNode, rows []map[string]any, keyOf func(string) string) error {
	if !readHooksEnabled(ctx) || len(nodes) == 0 || len(rows) == 0 {
		return nil
	}
	for _, node := range mergeHookRelations(nodes) {
		if node == nil || node.Target == nil {
			continue
		}
		childKeyOf := ch.wireConverterOf(node.Target)
		childRows := attachedRows(rows, keyOf(node.Relation.Name))
		if len(childRows) == 0 {
			continue
		}
		maskRows(hookCtx(ctx), node.Target, childKeyOf, childRows)
		var reg *hook.HookRegistry
		if ch.ChildHooks != nil {
			reg = ch.ChildHooks(node.Target.GetName())
		}
		if reg == nil {
			if err := ch.applyChildReadHooksUnder(ctx, node.Children, childRows, childKeyOf); err != nil {
				return err
			}
			continue
		}
		if len(reg.HooksFor(hook.AfterList)) > 0 {
			before := append([]map[string]any(nil), childRows...)
			payload := &hook.ListPayload{
				Request: ch.requestFrom(ctx),
				Results: childRows,
			}
			if err := runHooks(reg, hookCtx(ctx), hook.AfterList, payload); err != nil {
				return fmt.Errorf("include %s: after-list hook: %w", node.Relation.Name, err)
			}
			conv := ch.wireConverterOf(node.Target)
			keys := node.Target.KeyColumns()
			wire := make([]string, len(keys))
			for i, k := range keys {
				wire[i] = conv(k)
			}
			if err := reattachHookResults(node.Relation.Name, wire, before, payload.Results); err != nil {
				return err
			}
			childRows = before
		}
		toOne := node.Relation.Type == entity.RelHasOne || node.Relation.Type == entity.RelManyToOne
		if toOne && len(reg.HooksFor(hook.AfterGet)) > 0 {
			req := ch.requestFrom(ctx)
			keys := node.Target.KeyColumns()
			conv := ch.wireConverterOf(node.Target)
			for i, child := range childRows {
				id, _ := entity.RowKey(child, keys, conv)
				payload := &hook.GetPayload{Request: req, ID: id, Result: child}
				if err := runHooks(reg, hookCtx(ctx), hook.AfterGet, payload); err != nil {
					return fmt.Errorf("include %s: after-get hook: %w", node.Relation.Name, err)
				}
				if err := foldHookRow(node.Relation.Name, i, child, payload.Result); err != nil {
					return err
				}
			}
		}
		if err := ch.applyChildReadHooksUnder(ctx, node.Children, childRows, childKeyOf); err != nil {
			return err
		}
	}
	return nil
}

func mergeHookRelations(nodes []*IncludeNode) []*IncludeNode {
	var merged []*IncludeNode
	byName := make(map[string]*IncludeNode)
	for _, node := range nodes {
		if node == nil || node.Target == nil {
			continue
		}
		if prior := byName[node.Relation.Name]; prior != nil {
			prior.Children = append(prior.Children, node.Children...)
			continue
		}
		copyNode := *node
		copyNode.Children = append([]*IncludeNode(nil), node.Children...)
		byName[node.Relation.Name] = &copyNode
		merged = append(merged, &copyNode)
	}
	return merged
}

// attachedRows collects the rows attached under key across rows, each map once.
func attachedRows(rows []map[string]any, key string) []map[string]any {
	seen := map[uintptr]bool{}
	var out []map[string]any
	add := func(m map[string]any) {
		if m == nil {
			return
		}
		p := reflect.ValueOf(m).Pointer()
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, m)
	}
	for _, row := range rows {
		switch v := row[key].(type) {
		case map[string]any:
			add(v)
		case []map[string]any:
			for _, m := range v {
				add(m)
			}
		case []any:
			for _, e := range v {
				if m, ok := e.(map[string]any); ok {
					add(m)
				}
			}
		}
	}
	return out
}

// runResponseHooks masks a deep copy of a write's response and runs the
// cascade children's read hooks and the entity's AfterGet over it.
func (ch *CrudHandler) runResponseHooks(r *http.Request, result map[string]any) (map[string]any, error) {
	parentHooks := ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterGet)) > 0
	if result == nil || (!parentHooks && !ch.hasMasks() && !ch.hasCascadeAttachments(result)) {
		return result, nil
	}
	row := deepcopy.Map(result)
	ch.maskOwn(r.Context(), row)
	if err := ch.applyCascadeChildReadHooks(withRealRequest(WithReadHooks(r.Context()), r), row); err != nil {
		return nil, err
	}
	if !parentHooks {
		return row, nil
	}
	id, _ := ch.rowID(row)
	payload := &hook.GetPayload{Request: r, ID: id, Result: row}
	if err := runHooks(ch.Hooks, hookCtx(r.Context()), hook.AfterGet, payload); err != nil {
		return nil, err
	}
	return payload.Result, nil
}

// reattachHookResults folds a child AfterList hook's output back into the
// attached rows by primary key.
func reattachHookResults(relation string, pkKeys []string, original, returned []map[string]any) error {
	pkKey := strings.Join(pkKeys, ", ")
	if len(returned) != len(original) {
		return fmt.Errorf(
			"include %s: after-list hook changed the row count (%d → %d); "+
				"an eager-loaded relation cannot drop or add rows because each is already "+
				"keyed to its parent: filter in the child's own BeforeList, or use Hidden",
			relation, len(original), len(returned))
	}
	origAt := make(map[uintptr]int, len(original))
	byPK := make(map[string][]int, len(original))
	pkUsable := len(pkKeys) > 0
	for i, row := range original {
		origAt[reflect.ValueOf(row).Pointer()] = i
		if !pkUsable {
			continue
		}
		id, ok := entity.RowKey(row, pkKeys, nil)
		if !ok {
			pkUsable = false
			continue
		}
		byPK[id] = append(byPK[id], i)
	}

	type replacement struct {
		at   int
		want map[string]any
	}
	var folds []replacement
	claimed := make(map[int]bool, len(original))
	for i, want := range returned {
		if want == nil {
			return fmt.Errorf("include %s: after-list hook nil'd row %d", relation, i)
		}
		at, ok := origAt[reflect.ValueOf(want).Pointer()]
		if !ok {
			if !pkUsable {
				return fmt.Errorf(
					"include %s: after-list hook replaced a row, but these rows carry no %q to "+
						"match it against: mutate the row in place instead",
					relation, pkKey)
			}
			v, has := entity.RowKey(want, pkKeys, nil)
			if !has {
				return fmt.Errorf(
					"include %s: after-list hook returned a row without its %q, so it cannot be "+
						"matched to the parent that references it: keep the id when projecting",
					relation, pkKey)
			}
			at, ok = -1, false
			for _, cand := range byPK[v] {
				if !claimed[cand] {
					at, ok = cand, true
					break
				}
			}
			if !ok {
				if _, known := byPK[v]; known {
					return fmt.Errorf(
						"include %s: after-list hook returned %s=%v more often than the query did; "+
							"each eager-loaded row is keyed to its parent and must appear once",
						relation, pkKey, v)
				}
				return fmt.Errorf(
					"include %s: after-list hook returned a row (%s=%v) that was not among the "+
						"eager-loaded rows; an include cannot introduce rows",
					relation, pkKey, v)
			}
			folds = append(folds, replacement{at: at, want: want})
		}
		if claimed[at] {
			return fmt.Errorf(
				"include %s: after-list hook returned the same row twice; each eager-loaded row "+
					"is keyed to its parent and must appear once",
				relation)
		}
		claimed[at] = true
	}
	for _, f := range folds {
		if err := foldHookRow(relation, f.at, original[f.at], f.want); err != nil {
			return err
		}
	}
	return nil
}

// foldHookRow copies a replacement row's contents into the map the parents
// reference.
func foldHookRow(relation string, i int, row, want map[string]any) error {
	if want == nil {
		return fmt.Errorf("include %s: read hook nil'd row %d", relation, i)
	}
	if sameMap(row, want) {
		return nil
	}
	for k := range row {
		delete(row, k)
	}
	maps.Copy(row, want)
	return nil
}

func sameMap(a, b map[string]any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func (ch *CrudHandler) hasCascadeAttachments(result map[string]any) bool {
	if ch.Entity == nil {
		return false
	}
	for _, rel := range ch.Entity.Config.Relations {
		if _, ok := result[ch.convertKey(rel.Name)]; ok && rel.CascadeWrite {
			return true
		}
	}
	return false
}

// applyCascadeChildReadHooks runs each cascade-written child's masks and
// read hooks over a write response under WithReadHooks.
func (ch *CrudHandler) applyCascadeChildReadHooks(ctx context.Context, result map[string]any) error {
	if !readHooksEnabled(ctx) || len(result) == 0 || ch.Entity == nil {
		return nil
	}
	rows := []map[string]any{result}
	nodes := ch.cascadeNodes(ch.Entity, ch.convertKey, rows)
	if len(nodes) == 0 {
		return nil
	}
	return ch.applyChildReadHooks(ctx, nodes, rows)
}

// cascadeNodes is the include tree of the cascade relations attached to rows,
// at every depth written.
func (ch *CrudHandler) cascadeNodes(ent *entity.Entity, keyOf func(string) string, rows []map[string]any) []*IncludeNode {
	var nodes []*IncludeNode
	for _, rel := range ent.Config.Relations {
		if !rel.CascadeWrite {
			continue
		}
		childRows := attachedRows(rows, keyOf(rel.Name))
		if len(childRows) == 0 {
			continue
		}
		target, err := entity.ResolveTarget(ch.Registry, ent, rel.Entity)
		if err != nil || target == nil {
			continue
		}
		nodes = append(nodes, &IncludeNode{
			Name:     rel.Name,
			Relation: rel,
			Target:   target,
			Children: ch.cascadeNodes(target, ch.wireConverterOf(target), childRows),
		})
	}
	return nodes
}
