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

// The in-process read API (ListAll, CountAll, GetOne) hands back STORED
// values. That is the right default for a Go API whose callers are trusted
// server code: it reads what is in the database, and read-modify-write works.
//
// It is the wrong default for one caller, generated screens. An app spec
// app renders its grid and detail page through ListAll/GetOne, to an end
// user, so `GET /cards` returning "****1111" while the app's own page printed
// the stored number was a real disclosure.
//
// So this is opt-IN, per call, at the display sites. Making it the default
// instead breaks every caller that reads a value to write it back: a typed
// repo's Get→Update round trip persists the mask over the real column, seed
// reference resolution stops matching, dashboard aggregates sum masked
// numerics to zero, and an AfterGet hook that looks up its own entity
// re-enters itself until the stack is exhausted, which recover() cannot
// catch. All five were observed.

type readHooksKey struct{}

// realRequestKey carries the in-flight *http.Request down to child read
// hooks. A redactor is allowed to branch on ListPayload.Request (a role
// header, a cookie), and handing it a synthetic request made the same hook
// behave differently on ?include=card than on GET /cards.
type realRequestKey struct{}

// withRealRequest tags ctx with the request currently being served.
func withRealRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, realRequestKey{}, r)
}

// requestFrom returns the in-flight request, falling back to a synthetic one
// carrying ctx when there is none (the in-process API).
//
// The synthetic request carries hookCtx(ctx), not ctx. Hooks are handed the
// request so they can branch on it, and threading p.Request.Context() into a
// further read is ordinary style, but that context reaching a hook with the
// opt-in still set is the self-recursion hookCtx exists to prevent, just
// arriving by the other door. Stripping the ctx argument alone left the
// synthetic request as a live path back into the same hook: observed at 201
// re-entries and then a stack overflow, which recover() cannot catch.
func (ch *CrudHandler) requestFrom(ctx context.Context) *http.Request {
	if r, ok := ctx.Value(realRequestKey{}).(*http.Request); ok && r != nil {
		return r
	}
	return syntheticRequest(hookCtx(ctx), http.MethodGet, "/")
}

// WithReadHooks returns a context in which the in-process read API runs the
// read hooks the HTTP surface runs, so callers see the rows and values a
// GET would return: BeforeList/BeforeGet scope the query (ListAll and
// CountAll run BeforeList, GetOne runs BeforeGet), and AfterList/AfterGet
// mask the result.
//
// Use it where rows are RENDERED, not where they are read to be written back:
//
//	rows, err := ch.ListAll(crud.WithReadHooks(ctx), opts)   // a screen
//	row, err := ch.GetOne(ctx, id, nil)                      // an edit form
//
// Do not set it on a context handed to a hook: an AfterGet hook that reads
// its own entity through a hook-applying context recurses until the process
// dies. HTTP request handling is unaffected, those paths always run hooks.
func WithReadHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, readHooksKey{}, true)
}

// readHooksEnabled reports whether ctx opted in via WithReadHooks.
func readHooksEnabled(ctx context.Context) bool {
	v, _ := ctx.Value(readHooksKey{}).(bool)
	return v
}

// hookCtx strips the opt-in before a hook runs, so a hook that reads its own
// entity gets stored values instead of re-entering itself. Without this the
// recursion is unbounded and ends in stack exhaustion, which is a fatal
// runtime error, runHookSafely's recover() cannot catch it.
func hookCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, readHooksKey{}, false)
}

// runBeforeList runs the BeforeList chain for an in-process read under
// WithReadHooks and returns the WHERE clauses its hooks appended. The
// HTTP List handler runs the same chain and ANDs the same clauses into
// both its data and count queries; a hook error refuses the read there
// with a 400 and refuses it here with the error.
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

// childBeforeScopes runs a related entity's OWN read-scoping hooks for a read
// that reaches its rows from another entity's route (?include=, ?rel.field=)
// and returns the WHERE clauses they appended.
//
// BeforeList and BeforeGet are the documented way to scope reads, and the
// direct routes apply them; an include or a nested filter that skipped them
// served, or counted, every row the child's own route hides. The chains are
// picked the way applyChildReadHooks picks the After side: BeforeList
// always, plus BeforeGet for a to-one relation, which mirrors GET
// /child/{id}. The rows are loaded in one batch, so BeforeGet sees an empty
// ID: a hook that needs one should refuse, and its error fails the request.
//
// Gated like the parent read: always on an HTTP request, and in process only
// under WithReadHooks, where ListAll runs the parent's own BeforeList.
func (ch *CrudHandler) childBeforeScopes(ctx context.Context, target *entity.Entity, rel entity.Relation) ([]hook.WhereClause, error) {
	return ch.childBeforeScopesFor(ctx, target, rel, "")
}

// childBeforeScopesFor is childBeforeScopes for a read that names one
// target row: a to-one relation's BeforeGet sees its id, as GET
// /child/{id} would hand it.
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
// through ?include= or a nested ?rel.field= filter. writeIncludeError
// renders it the way the child's own route renders the same refusal (400
// with the hook's wording or phrase), so a hook that refuses an ID-less
// BeforeGet reads as a refusal, not a server fault. Error() keeps the
// relation and chain for logs and in-process callers.
type childHookError struct {
	rel, chain string
	err        error
}

func (e *childHookError) Error() string {
	return fmt.Sprintf("relation %s: %s hook: %v", e.rel, e.chain, e.err)
}

func (e *childHookError) Unwrap() error { return e.err }

// childHookRegistry looks up name's hook registry through ChildHooks, a
// host-installable lookup that the nested-filter path reaches from the
// in-process API. A panic there is turned into an error, so the read fails
// closed instead of skipping the scope or killing the process.
func (ch *CrudHandler) childHookRegistry(name string) (reg *hook.HookRegistry, err error) {
	defer func() {
		if r := recover(); r != nil {
			reg, err = nil, fmt.Errorf("child hooks for %s: %s", name, textsafe.Recovered(r))
		}
	}()
	return ch.ChildHooks(name), nil
}

// applyChildBeforeHooks stores the include target's Before-hook clauses on
// node.HookScopes, and a many_to_many join entity's BeforeList clauses on
// node.PivotHookScopes. See childBeforeScopes.
func (ch *CrudHandler) applyChildBeforeHooks(ctx context.Context, node *IncludeNode) error {
	if node == nil || !readHooksEnabled(ctx) {
		return nil
	}
	where, err := ch.childBeforeScopes(ctx, node.Target, node.Relation)
	if err != nil {
		return err
	}
	node.HookScopes = where
	// The link rows are the join entity's own rows, and GET /<pivot> runs
	// its BeforeList over them. A relation reaching them from the parent
	// is to-many, so BeforeGet never applies. A migrator-made pivot (nil)
	// has no hooks.
	pivotWhere, err := ch.childBeforeScopes(ctx, node.Pivot, node.Relation)
	if err != nil {
		return err
	}
	node.PivotHookScopes = pivotWhere
	return nil
}

// pivotHookScope renders a join entity's BeforeList clauses as one predicate
// on its Through table's rows, keyed by the join entity's own key the way
// hookScopeSubquery keys a target. Numbered from $1.
func pivotHookScope(pivot *entity.Entity, through string, clauses []hook.WhereClause) (string, []any) {
	if pivot == nil || len(clauses) == 0 {
		return "", nil
	}
	return hookScopeSubquery(through, through, pivot.KeyColumns(), clauses)
}

// hookScopeSubquery renders Before-hook clauses as one predicate on the rows
// of table, as qualifier names them:
//
//	qualifier.id IN (SELECT id FROM table WHERE (c1) AND (c2))
//
// A hook's clause is written against GET /<target>, a bare SELECT on the
// target table, so it is evaluated in exactly that shape rather than spliced
// into a JOIN where its bare column names could be ambiguous or bind to the
// wrong table. Placeholders are numbered from $1 (query.Build renumbers the
// clauses the way the direct route does), so a caller that runs the SQL as
// is puts this predicate FIRST and numbers the rest after len(args).
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
		lhs[i] = qualifier + "." + k
	}
	left := lhs[0]
	if len(lhs) > 1 {
		left = "(" + strings.Join(lhs, ", ") + ")"
	}
	return left + " IN (" + sub + ")", args
}

// runAfterGet applies the AfterGet chain to a single in-process result and
// returns the (possibly replaced) row.
//
// A hook error is returned to the caller rather than logged and swallowed:
// the in-process caller can decide what to do, and handing back unredacted
// data because a redaction hook failed is exactly the outcome this exists to
// prevent. Request is the synthetic request the surrounding scope already
// builds, so hooks reading p.Request keep working.
func (ch *CrudHandler) runAfterGet(ctx context.Context, r *http.Request, id string, result map[string]any) (map[string]any, error) {
	if ch.Hooks == nil || !readHooksEnabled(ctx) || result == nil {
		return result, nil
	}
	payload := &hook.GetPayload{Request: hookRequest(r), ID: id, Result: result}
	if err := runHooks(ch.Hooks, hookCtx(ctx), hook.AfterGet, payload); err != nil {
		return nil, fmt.Errorf("after-get hook: %w", err)
	}
	return payload.Result, nil
}

// identityOnly reduces a write response to its primary key.
//
// It is what a write returns when the response hook fails. The alternatives
// are both wrong: echoing the row serves exactly the values the failed hook
// was there to mask, and answering 500 tells the caller a write that COMMITTED
// did not happen, so it retries and creates the row twice, or, in a batch,
// loses the ids of rows that are already in the table. Reporting the write's
// outcome truthfully while withholding everything the hook did not get to
// vouch for keeps both properties. The id is not a secret: it is the row's own
// URL, and the caller is about to be redirected to it.
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

// hookRequest strips the read-hook opt-in from the request a hook receives.
//
// hookCtx covers the ctx ARGUMENT. A hook also gets payload.Request, and
// threading p.Request.Context() into its own read is ordinary style, so on
// the in-process path, where that request is synthesised from the caller's
// opted-in context, the hook re-entered itself through it and exhausted the
// stack. Real HTTP requests never carry the opt-in, so this is a no-op there
// and the request identity is preserved.
func hookRequest(r *http.Request) *http.Request {
	if r == nil || !readHooksEnabled(r.Context()) {
		return r
	}
	return r.WithContext(hookCtx(r.Context()))
}

// runAfterList applies the AfterList chain to an in-process result set.
func (ch *CrudHandler) runAfterList(ctx context.Context, r *http.Request, results []map[string]any) ([]map[string]any, error) {
	if ch.Hooks == nil || !readHooksEnabled(ctx) {
		return results, nil
	}
	payload := &hook.ListPayload{Request: hookRequest(r), Results: results}
	if err := runHooks(ch.Hooks, hookCtx(ctx), hook.AfterList, payload); err != nil {
		return nil, fmt.Errorf("after-list hook: %w", err)
	}
	return payload.Results, nil
}

// applyChildReadHooks runs each eager-loaded relation's OWN entity AfterList
// over the rows attached to `rows`, then recurses into deeper includes.
//
// It runs AFTER key conversion, on the attached values, for a reason worth
// stating: the eager loader produces raw DB column names, while the child's
// own list endpoint returns JSON-cased ones. A hook written against what
// `GET /children` actually returns (`cardNumber`) does not match `card_number`,
// so hooking the pre-conversion rows either no-ops or leaves both keys in the
// map and lets randomized map iteration pick the winner. Hooking the
// converted maps means the hook sees exactly the keys it sees everywhere else.
//
// Rows are deduped by pointer: a belongs-to target attached to many parents is
// one map, and must be masked once.
//
// A hook may redact in place OR by replacing an element with a projection
// (`p.Results[i] = redactedCopy`), both are shapes the typed API documents.
// The loader has already keyed each row to its parent, so a replacement is
// folded back into the original map rather than swapped; a hook that changes
// the ROW COUNT cannot be honoured that way and fails the request instead of
// silently serving the rows it tried to drop.
//
// Gated on the same opt-in as the parent read, so an in-process caller that
// asked for stored values gets them for included rows too. The HTTP handlers
// mark their context, so a request always redacts; without the gate an
// in-process read returned raw top-level fields alongside masked included
// ones, and code that wrote an included row back persisted the mask.
func (ch *CrudHandler) applyChildReadHooks(ctx context.Context, nodes []*IncludeNode, rows []map[string]any) error {
	return ch.applyChildReadHooksUnder(ctx, nodes, rows, ch.convertKey)
}

// applyChildReadHooksUnder is applyChildReadHooks over rows whose keys
// convert with keyOf. A relation's rows sit under the key its PARENT hop's
// wire map gives it, so a nested level looks them up with the parent
// target's converter, never the root handler's: a WireName on either side
// otherwise misses the rows, and the child's masking never runs.
func (ch *CrudHandler) applyChildReadHooksUnder(ctx context.Context, nodes []*IncludeNode, rows []map[string]any, keyOf func(string) string) error {
	if ch.ChildHooks == nil || !readHooksEnabled(ctx) || len(nodes) == 0 || len(rows) == 0 {
		return nil
	}
	for _, node := range nodes {
		if node == nil || node.Target == nil {
			continue
		}
		childKeyOf := ch.wireConverterOf(node.Target)
		childRows := attachedRows(rows, keyOf(node.Relation.Name))
		if len(childRows) == 0 {
			continue
		}
		reg := ch.ChildHooks(node.Target.GetName())
		if reg == nil {
			if err := ch.applyChildReadHooksUnder(ctx, node.Children, childRows, childKeyOf); err != nil {
				return err
			}
			continue
		}
		if len(reg.HooksFor(hook.AfterList)) > 0 {
			// Snapshot the row pointers BEFORE the hook runs. payload.Results
			// shares childRows' backing array, so a hook assigning
			// p.Results[i] overwrites our own slice, comparing the two
			// afterwards would find them identical and conclude nothing
			// changed, which is exactly the silent no-op being fixed.
			before := append([]map[string]any(nil), childRows...)
			payload := &hook.ListPayload{
				Request: ch.requestFrom(ctx),
				Results: childRows,
			}
			if err := runHooks(reg, hookCtx(ctx), hook.AfterList, payload); err != nil {
				return fmt.Errorf("include %s: after-list hook: %w", node.Relation.Name, err)
			}
			// Included rows carry the target's wire keys, WireName
			// overrides included, not this handler's.
			conv := ch.wireConverterOf(node.Target)
			keys := node.Target.KeyColumns()
			wire := make([]string, len(keys))
			for i, k := range keys {
				wire[i] = conv(k)
			}
			if err := reattachHookResults(node.Relation.Name, wire, before, payload.Results); err != nil {
				return err
			}
			// Recurse over the ORIGINAL maps: those are what the parents
			// reference, and reattach has folded any replacement into them.
			childRows = before
		}
		// A to-one relation serialises as a single object, so the surface it
		// mirrors is GET /child/{id}, which runs AfterGet, not AfterList. An
		// app that masks in AfterGet only is entirely consistent with its own
		// routes and still served the stored value through ?include=author.
		// Both chains run here rather than one replacing the other: picking by
		// arity alone would leak the mirror-image case, where the mask lives
		// in AfterList. A hook registered on both runs twice on these rows, so
		// masks should be idempotent, the documented ones (assign a constant,
		// delete the key) are.
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
				// A hook that replaced the map instead of mutating it needs the
				// same fold-back as the list path: the parents reference the
				// original.
				if err := foldHookRow(node.Relation.Name, i, child, payload.Result); err != nil {
					return err
				}
			}
		}
		// Deeper levels: this node's rows are the parents for its children,
		// and they carry this target's wire keys.
		if err := ch.applyChildReadHooksUnder(ctx, node.Children, childRows, childKeyOf); err != nil {
			return err
		}
	}
	return nil
}

// attachedRows collects the related rows attached under key across rows: a
// to-one map, or the elements of a to-many slice. Rows are deduped by
// pointer: a belongs-to target attached to many parents is one map, and must
// be masked once.
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

// runResponseHooks applies AfterGet to a write's response body.
//
// A create or update returns the row RETURNING produced, every visible
// column, including ones the caller never sent. AfterCreate/AfterUpdate run
// over it, but those are write hooks; a redaction registered on AfterGet does
// not, so a partial PUT used to echo back stored values for fields that GET
// masks. Applying it here keeps "what the HTTP surface returns" consistent
// across reads and writes without touching the in-process API, which stays
// raw so read-modify-write still works.
func (ch *CrudHandler) runResponseHooks(r *http.Request, result map[string]any) (map[string]any, error) {
	parentHooks := ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterGet)) > 0
	if result == nil || (!parentHooks && !ch.hasCascadeAttachments(result)) {
		return result, nil
	}
	// Redact a DEEP copy. The caller has already handed `result` to EmitEvent,
	// which passes it to an async goroutine that marshals it, the live bus,
	// the fanout tap, the webhook bridge, any Events.On handler. An in-place
	// hook here would write that map while those read it, which is a
	// concurrent map read/write: a runtime throw the bus's recover() cannot
	// catch. It also made the event payload's contents depend on who won the
	// race.
	//
	// A shallow copy is not enough, and shipping one was the same bug wearing
	// a hat: every nested map and slice stayed shared with the bus's record,
	// so a hook masking a field INSIDE an embedded object (row["profile"]
	// ["ssn"], the ordinary shape for a computed sub-document) wrote straight
	// through the copy into the event lane, deterministically contaminating
	// it and racing the subscriber that reads it. deepcopy.Map is the same
	// helper redactEventRecord uses, for the same reason.
	row := deepcopy.Map(result)
	// Cascade-written children get their own entity's read hooks here, on
	// the copy, after commit: running them inside the write handed the
	// redacted child to AfterCreate/AfterUpdate, the outbox and the event.
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

// reattachHookResults folds a child AfterList hook's output back into the maps
// the include loader has already attached to their parents.
//
// The parent read path can simply take payload.Results, because it owns the
// slice it is about to serialise. An include cannot: each row is already
// referenced from one or more parent rows, so replacing the slice would leave
// those references pointing at the pre-hook maps, the hook would appear to
// run and change nothing, which is the silent-fail-open shape this whole
// change exists to remove.
//
// Rows are matched by PRIMARY KEY, not by position and not by pointer.
//
// Position was the first attempt: it corrupts the moment a hook reorders,
// writing each row's contents into a different parent's attachment and, since
// it mutates originals that later iterations still read as sources, turning
// [A,B,C] → [C,B,A] into [C,B,C], one row duplicated, one destroyed, one
// attributed to a parent whose own foreign key names a different row, served
// with a 200.
//
// Pointer identity was the second: it recognises a reorder only when the hook
// mutated in place. The documented alternative, replacing an element with a
// redacted copy, yields fresh maps, so a hook that projects AND sorts looked
// like a slice of new rows and folded positionally, reproducing the identical
// corruption. It also made a legitimate sorting hook a hard 500, which is
// data-dependent: whether a request works then depends on the stored rows, so
// anyone able to insert a child row could permanently break another user's
// include.
//
// The primary key is what actually identifies a row across both shapes:
// pkKeys names its columns as the rows spell them, and a composite key
// matches on the record id they join to. Every
// documented mask preserves it (assign a constant, delete the masked key), so
// matching on it accepts in-place edits, projections, and any ordering, while
// still refusing the cases that cannot be honoured: a changed row count, a
// duplicate, a row the loader never produced, or a replacement that dropped
// its own id. Ordering is a no-op either way, what the client sees comes from
// the attachment the loader built, which this function never touches.
//
// Nothing is mutated until every element has been matched, so a refused
// payload is left exactly as it was.
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
	// One id can map to SEVERAL rows. eagerLoadManyToMany builds a fresh map
	// per JOIN row, so a child shared by two parents in the same page arrives
	// as two distinct maps carrying the same primary key, that is what a
	// many-to-many IS. A single-valued index kept only the last, so both
	// replacements resolved to one row, the second tripped the duplicate
	// refusal, and a documented hook shape 500'd as soon as two parents shared
	// a child. The duplicates are the same DB row with identical content, so
	// which one a given replacement folds into does not matter; what matters
	// is that each claims a distinct slot.
	byPK := make(map[string][]int, len(original))
	pkUsable := len(pkKeys) > 0
	for i, row := range original {
		origAt[reflect.ValueOf(row).Pointer()] = i
		if !pkUsable {
			continue
		}
		id, ok := entity.RowKey(row, pkKeys, nil)
		if !ok {
			pkUsable = false // no id on these rows; identity is all we have
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
			// A replacement. Only its id can say which row it stands for.
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
			// First slot for this id that nothing has claimed yet.
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
// already reference. A hook that mutated in place is a no-op here.
func foldHookRow(relation string, i int, row, want map[string]any) error {
	if want == nil {
		return fmt.Errorf("include %s: read hook nil'd row %d", relation, i)
	}
	if sameMap(row, want) {
		return nil // mutated in place; nothing to fold back
	}
	for k := range row {
		delete(row, k)
	}
	maps.Copy(row, want)
	return nil
}

// sameMap reports whether two maps are the same underlying object.
func sameMap(a, b map[string]any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// hasCascadeAttachments reports whether a write result carries any
// cascade-written relation that child read hooks could apply to.
func (ch *CrudHandler) hasCascadeAttachments(result map[string]any) bool {
	if ch.ChildHooks == nil || ch.Entity == nil {
		return false
	}
	for _, rel := range ch.Entity.Config.Relations {
		if _, ok := result[ch.convertKey(rel.Name)]; ok && rel.CascadeWrite {
			return true
		}
	}
	return false
}

// applyCascadeChildReadHooks runs child entity read hooks over cascade-written
// relation attachments in a write response. runResponseHooks calls it on a
// deep copy after commit; it must never run on the row the write is still
// using. No-op without ChildHooks or when read hooks are not enabled on ctx.
func (ch *CrudHandler) applyCascadeChildReadHooks(ctx context.Context, result map[string]any) error {
	if ch.ChildHooks == nil || !readHooksEnabled(ctx) || len(result) == 0 || ch.Entity == nil {
		return nil
	}
	rows := []map[string]any{result}
	nodes := ch.cascadeNodes(ch.Entity, ch.convertKey, rows)
	if len(nodes) == 0 {
		return nil
	}
	return ch.applyChildReadHooks(ctx, nodes, rows)
}

// cascadeNodes builds the include tree a cascade write's response carries:
// one node per cascade relation of ent attached to rows, each with the nodes
// of the levels written beneath it. A nested write answers with its
// grandchildren under each child, and a flat tree ran only the children's
// hooks, so a field the grandchild's entity masks came back raw. The tree is
// read off the response itself, so it ends where the written rows end.
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
