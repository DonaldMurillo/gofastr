package crud

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The in-process read API (ListAll, CountAll, GetOne) hands back STORED
// values. That is the right default for a Go API whose callers are trusted
// server code: it reads what is in the database, and read-modify-write works.
//
// It is the wrong default for one caller — generated screens. A blueprint
// app renders its grid and detail page through ListAll/GetOne, to an end
// user, so `GET /cards` returning "****1111" while the app's own page printed
// the stored number was a real disclosure.
//
// So this is opt-IN, per call, at the display sites. Making it the default
// instead breaks every caller that reads a value to write it back: a typed
// repo's Get→Update round trip persists the mask over the real column, seed
// reference resolution stops matching, dashboard aggregates sum masked
// numerics to zero, and an AfterGet hook that looks up its own entity
// re-enters itself until the stack is exhausted — which recover() cannot
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
// further read is ordinary style — but that context reaching a hook with the
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

// WithReadHooks returns a context in which the in-process read API applies
// AfterList/AfterGet, so callers see the same values the HTTP surface returns.
//
// Use it where rows are RENDERED, not where they are read to be written back:
//
//	rows, err := ch.ListAll(crud.WithReadHooks(ctx), opts)   // a screen
//	row, err := ch.GetOne(ctx, id, nil)                      // an edit form
//
// Do not set it on a context handed to a hook: an AfterGet hook that reads
// its own entity through a hook-applying context recurses until the process
// dies. HTTP request handling is unaffected — those paths always run hooks.
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
// runtime error — runHookSafely's recover() cannot catch it.
func hookCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, readHooksKey{}, false)
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
	if err := ch.Hooks.ExecuteHooks(hookCtx(ctx), hook.AfterGet, payload); err != nil {
		return nil, fmt.Errorf("after-get hook: %w", err)
	}
	return payload.Result, nil
}

// identityOnly reduces a write response to its primary key.
//
// It is what a write returns when the response hook fails. The alternatives
// are both wrong: echoing the row serves exactly the values the failed hook
// was there to mask, and answering 500 tells the caller a write that COMMITTED
// did not happen — so it retries and creates the row twice, or, in a batch,
// loses the ids of rows that are already in the table. Reporting the write's
// outcome truthfully while withholding everything the hook did not get to
// vouch for keeps both properties. The id is not a secret: it is the row's own
// URL, and the caller is about to be redirected to it.
func (ch *CrudHandler) identityOnly(result map[string]any) map[string]any {
	key := ch.convertKey(ch.PrimaryKey)
	if v, ok := result[key]; ok {
		return map[string]any{key: v}
	}
	return map[string]any{}
}

// hookRequest strips the read-hook opt-in from the request a hook receives.
//
// hookCtx covers the ctx ARGUMENT. A hook also gets payload.Request, and
// threading p.Request.Context() into its own read is ordinary style — so on
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
	if err := ch.Hooks.ExecuteHooks(hookCtx(ctx), hook.AfterList, payload); err != nil {
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
// (`p.Results[i] = redactedCopy`) — both are shapes the typed API documents.
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
	if ch.ChildHooks == nil || !readHooksEnabled(ctx) || len(nodes) == 0 || len(rows) == 0 {
		return nil
	}
	for _, node := range nodes {
		if node == nil || node.Target == nil {
			continue
		}
		outKey := ch.convertKey(node.Relation.Name)
		seen := map[uintptr]bool{}
		var childRows []map[string]any
		add := func(m map[string]any) {
			if m == nil {
				return
			}
			p := reflect.ValueOf(m).Pointer()
			if seen[p] {
				return
			}
			seen[p] = true
			childRows = append(childRows, m)
		}
		for _, row := range rows {
			switch v := row[outKey].(type) {
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
		if len(childRows) == 0 {
			continue
		}
		reg := ch.ChildHooks(node.Target.GetName())
		if reg == nil {
			if err := ch.applyChildReadHooks(ctx, node.Children, childRows); err != nil {
				return err
			}
			continue
		}
		if len(reg.HooksFor(hook.AfterList)) > 0 {
			// Snapshot the row pointers BEFORE the hook runs. payload.Results
			// shares childRows' backing array, so a hook assigning
			// p.Results[i] overwrites our own slice — comparing the two
			// afterwards would find them identical and conclude nothing
			// changed, which is exactly the silent no-op being fixed.
			before := append([]map[string]any(nil), childRows...)
			payload := &hook.ListPayload{
				Request: ch.requestFrom(ctx),
				Results: childRows,
			}
			if err := reg.ExecuteHooks(hookCtx(ctx), hook.AfterList, payload); err != nil {
				return fmt.Errorf("include %s: after-list hook: %w", node.Relation.Name, err)
			}
			if err := reattachHookResults(node.Relation.Name, ch.convertKey(node.Target.PrimaryKey), before, payload.Results); err != nil {
				return err
			}
			// Recurse over the ORIGINAL maps: those are what the parents
			// reference, and reattach has folded any replacement into them.
			childRows = before
		}
		// A to-one relation serialises as a single object, so the surface it
		// mirrors is GET /child/{id} — which runs AfterGet, not AfterList. An
		// app that masks in AfterGet only is entirely consistent with its own
		// routes and still served the stored value through ?include=author.
		// Both chains run here rather than one replacing the other: picking by
		// arity alone would leak the mirror-image case, where the mask lives
		// in AfterList. A hook registered on both runs twice on these rows, so
		// masks should be idempotent — the documented ones (assign a constant,
		// delete the key) are.
		toOne := node.Relation.Type == entity.RelHasOne || node.Relation.Type == entity.RelManyToOne
		if toOne && len(reg.HooksFor(hook.AfterGet)) > 0 {
			req := ch.requestFrom(ctx)
			pkKey := ch.convertKey(node.Target.PrimaryKey)
			for i, child := range childRows {
				id := ""
				if v, ok := child[pkKey]; ok {
					id = fmt.Sprint(v)
				}
				payload := &hook.GetPayload{Request: req, ID: id, Result: child}
				if err := reg.ExecuteHooks(hookCtx(ctx), hook.AfterGet, payload); err != nil {
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
		// and they are already converted, so the same key logic applies.
		if err := ch.applyChildReadHooks(ctx, node.Children, childRows); err != nil {
			return err
		}
	}
	return nil
}

// runResponseHooks applies AfterGet to a write's response body.
//
// A create or update returns the row RETURNING produced — every visible
// column, including ones the caller never sent. AfterCreate/AfterUpdate run
// over it, but those are write hooks; a redaction registered on AfterGet does
// not, so a partial PUT used to echo back stored values for fields that GET
// masks. Applying it here keeps "what the HTTP surface returns" consistent
// across reads and writes without touching the in-process API, which stays
// raw so read-modify-write still works.
func (ch *CrudHandler) runResponseHooks(r *http.Request, result map[string]any) (map[string]any, error) {
	if ch.Hooks == nil || result == nil || len(ch.Hooks.HooksFor(hook.AfterGet)) == 0 {
		return result, nil
	}
	// Redact a DEEP copy. The caller has already handed `result` to EmitEvent,
	// which passes it to an async goroutine that marshals it — the live bus,
	// the fanout tap, the webhook bridge, any Events.On handler. An in-place
	// hook here would write that map while those read it, which is a
	// concurrent map read/write: a runtime throw the bus's recover() cannot
	// catch. It also made the event payload's contents depend on who won the
	// race.
	//
	// A shallow copy is not enough, and shipping one was the same bug wearing
	// a hat: every nested map and slice stayed shared with the bus's record,
	// so a hook masking a field INSIDE an embedded object (row["profile"]
	// ["ssn"] — the ordinary shape for a computed sub-document) wrote straight
	// through the copy into the event lane, deterministically contaminating
	// it and racing the subscriber that reads it. deepCopyRecord is the same
	// helper redactEventRecord uses, for the same reason.
	row := deepCopyRecord(result)
	id := ""
	if v, ok := row[ch.convertKey(ch.PrimaryKey)]; ok {
		id = fmt.Sprint(v)
	}
	payload := &hook.GetPayload{Request: r, ID: id, Result: row}
	if err := ch.Hooks.ExecuteHooks(hookCtx(r.Context()), hook.AfterGet, payload); err != nil {
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
// those references pointing at the pre-hook maps — the hook would appear to
// run and change nothing, which is the silent-fail-open shape this whole
// change exists to remove.
//
// Rows are matched by PRIMARY KEY, not by position and not by pointer.
//
// Position was the first attempt: it corrupts the moment a hook reorders,
// writing each row's contents into a different parent's attachment and, since
// it mutates originals that later iterations still read as sources, turning
// [A,B,C] → [C,B,A] into [C,B,C] — one row duplicated, one destroyed, one
// attributed to a parent whose own foreign key names a different row, served
// with a 200.
//
// Pointer identity was the second: it recognises a reorder only when the hook
// mutated in place. The documented alternative — replacing an element with a
// redacted copy — yields fresh maps, so a hook that projects AND sorts looked
// like a slice of new rows and folded positionally, reproducing the identical
// corruption. It also made a legitimate sorting hook a hard 500, which is
// data-dependent: whether a request works then depends on the stored rows, so
// anyone able to insert a child row could permanently break another user's
// include.
//
// The primary key is what actually identifies a row across both shapes. Every
// documented mask preserves it (assign a constant, delete the masked key), so
// matching on it accepts in-place edits, projections, and any ordering, while
// still refusing the cases that cannot be honoured: a changed row count, a
// duplicate, a row the loader never produced, or a replacement that dropped
// its own id. Ordering is a no-op either way — what the client sees comes from
// the attachment the loader built, which this function never touches.
//
// Nothing is mutated until every element has been matched, so a refused
// payload is left exactly as it was.
func reattachHookResults(relation, pkKey string, original, returned []map[string]any) error {
	if len(returned) != len(original) {
		return fmt.Errorf(
			"include %s: after-list hook changed the row count (%d → %d); "+
				"an eager-loaded relation cannot drop or add rows because each is already "+
				"keyed to its parent — filter in the child's own BeforeList, or use Hidden",
			relation, len(original), len(returned))
	}
	origAt := make(map[uintptr]int, len(original))
	// One id can map to SEVERAL rows. eagerLoadManyToMany builds a fresh map
	// per JOIN row, so a child shared by two parents in the same page arrives
	// as two distinct maps carrying the same primary key — that is what a
	// many-to-many IS. A single-valued index kept only the last, so both
	// replacements resolved to one row, the second tripped the duplicate
	// refusal, and a documented hook shape 500'd as soon as two parents shared
	// a child. The duplicates are the same DB row with identical content, so
	// which one a given replacement folds into does not matter; what matters
	// is that each claims a distinct slot.
	byPK := make(map[string][]int, len(original))
	pkUsable := pkKey != ""
	for i, row := range original {
		origAt[reflect.ValueOf(row).Pointer()] = i
		if !pkUsable {
			continue
		}
		v, ok := row[pkKey]
		if !ok {
			pkUsable = false // no id on these rows; identity is all we have
			continue
		}
		byPK[fmt.Sprint(v)] = append(byPK[fmt.Sprint(v)], i)
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
						"match it against — mutate the row in place instead",
					relation, pkKey)
			}
			v, has := want[pkKey]
			if !has {
				return fmt.Errorf(
					"include %s: after-list hook returned a row without its %q, so it cannot be "+
						"matched to the parent that references it — keep the id when projecting",
					relation, pkKey)
			}
			// First slot for this id that nothing has claimed yet.
			at, ok = -1, false
			for _, cand := range byPK[fmt.Sprint(v)] {
				if !claimed[cand] {
					at, ok = cand, true
					break
				}
			}
			if !ok {
				if _, known := byPK[fmt.Sprint(v)]; known {
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
	for k, v := range want {
		row[k] = v
	}
	return nil
}

// sameMap reports whether two maps are the same underlying object.
func sameMap(a, b map[string]any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}
