package crud

import (
	"net/http"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// registerLLMMDRoutes registers the /{path}/llm.md documentation endpoint for
// a single entity. Called automatically by RegisterCrudRoutes.
//
// It mounts the SCOPE-AWARE handler: the docs describe exactly the entity the
// sibling List route serves, so they answer to the same gate. Mounting the
// bare LLMMDHandler here checked only for a session, which let an
// authenticated caller with no read permission fetch the schema of an entity
// whose rows they get a 403 on.
//
// readOnly is the mount's own CrudRouteOptions.ReadOnly: the doc must
// describe the routes this registration actually mounts, so a read-only
// mount's llm.md omits the write and batch endpoints entirely (#358).
func registerLLMMDRoutes(r *router.Router, ch *CrudHandler, path string, readOnly bool) {
	path = NormalizePath(path)
	opts := LLMMDOptions{ReadOnly: readOnly}
	// BasePath is where the app mounted this handler (API prefix, group
	// prefix or version), so mcpBase is the path the page must name; path
	// alone is relative to r, which may be a group's sub-router (#503).
	if ch.BasePath != "" {
		opts.Path = ch.mcpBase()
	}
	r.Get(path+"/llm.md", LLMMDHandlerFor(ch, opts))
}

// CrudRouteOptions controls which routes are registered by RegisterCrudRoutes.
type CrudRouteOptions struct {
	NoLLMMD  bool // disable auto-generated /path/llm.md
	ReadOnly bool // register only the read routes (List/Get/events), for views and other read-only objects
	// States is the entity's states config, read by CrudRoutePatterns:
	// POST /path/{id}/transitions/{key} is mounted when it holds a
	// non-system move. RegisterCrudRoutes reads the handler's entity
	// instead, so it needs no value here.
	States *entity.StatesConfig
	// Key is the entity's primary-key columns, read by CrudRoutePatterns
	// to spell the record routes (KeyPath); nil means "id".
	// RegisterCrudRoutes reads the handler's key instead.
	Key []string
}

// KeyPath is the route tail naming one record of an entity keyed on
// cols: "/{id}" for one column, and one segment per column for a
// composite key, each wildcard named after its column, so
// "/{student_id}/{course_id}" serves /enrollments/s1/c9.
func KeyPath(cols []string) string {
	if len(cols) <= 1 {
		return "/{id}"
	}
	var b strings.Builder
	for _, c := range cols {
		b.WriteString("/{" + c + "}")
	}
	return b.String()
}

// TransitionPath is the route tail of a move on one record of an entity
// keyed on cols: KeyPath, then /transitions/{key}, the move's wildcard
// renamed when a key column holds the name "key".
func TransitionPath(cols []string) string {
	return KeyPath(cols) + "/transitions/{" + transitionParam(cols) + "}"
}

// transitionParam is the wildcard naming the move in
// <record>/transitions/{key}: "key", unless a key column holds that name.
func transitionParam(cols []string) string {
	p := "key"
	for len(cols) > 1 && slices.Contains(cols, p) {
		p += "_"
	}
	return p
}

// PathRecordID is the record id a request routed through KeyPath(cols)
// names, as entity.JoinKey renders it, for a handler mounted beside the
// CRUD routes (an admin restore, say). It is "" when a key segment is.
func PathRecordID(r *http.Request, cols []string) string {
	if len(cols) <= 1 {
		return r.PathValue("id")
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		if parts[i] = r.PathValue(c); parts[i] == "" {
			return ""
		}
	}
	return entity.JoinKey(parts)
}

// pathID is PathRecordID under the handler's key.
func (ch *CrudHandler) pathID(r *http.Request) string {
	return PathRecordID(r, ch.keyCols())
}

// CrudRoutePatterns returns the "METHOD /pattern" set RegisterCrudRoutes
// mounts for path, without mounting anything.
//
// It exists so a caller can pre-flight a collision BEFORE the first route
// is registered. App.TryEntity needs that: it mounts CRUD routes and then
// custom endpoints, so an endpoint shadowing one of these (a "_batch"
// endpoint on an entity at /posts) would panic mid-commit, after the
// registry entry and the CRUD routes are already published and with no
// way to un-publish them.
//
// TestCrudRoutePatternsMatchRegistration pins this list against what
// RegisterCrudRoutes actually registers, a route added to one and not
// the other fails that test rather than silently reopening the gap.
func CrudRoutePatterns(path string, opts ...CrudRouteOptions) []string {
	path = NormalizePath(path)

	var opt CrudRouteOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	rec := path + KeyPath(opt.Key)
	patterns := []string{
		"GET " + path,
		"GET " + rec,
	}
	if !opt.ReadOnly {
		patterns = append(patterns,
			"POST "+path,
			"PUT "+rec,
			"PATCH "+rec,
			"DELETE "+rec,
			"POST "+path+"/_batch",
			"PATCH "+path+"/_batch",
			"DELETE "+path+"/_batch",
		)
		if len(RoutableTransitions(opt.States)) > 0 {
			patterns = append(patterns, "POST "+path+TransitionPath(opt.Key))
		}
	}
	patterns = append(patterns, "GET "+path+"/_events")
	if !opt.NoLLMMD {
		patterns = append(patterns, "GET "+path+"/llm.md")
	}
	return patterns
}

// RegisterCrudRoutes registers the standard CRUD routes plus batch endpoints
// on the given router.
//
//	GET    /path             → List
//	GET    /path/{id}        → Get
//	POST   /path             → Create
//	PUT    /path/{id}        → Update
//	PATCH  /path/{id}        → Update (sparse)
//	DELETE /path/{id}        → Delete
//	POST   /path/_batch      → BatchCreate (atomic; all-or-nothing)
//	PATCH  /path/_batch      → BatchUpdate (atomic; all-or-nothing)
//	DELETE /path/_batch      → BatchDelete (atomic; all-or-nothing)
//	POST   /path/{id}/transitions/{key} → RunTransition (entities whose
//	                           States hold a non-system move)
//
// A composite key spells {id} one segment per key column (KeyPath).
//
// The batch routes use a "_batch" segment, which Go 1.22's ServeMux ranks
// above the wildcard /{id} so they take precedence over Get/Update/Delete
// on the same prefix.
func RegisterCrudRoutes(r *router.Router, handler *CrudHandler, path string, opts ...CrudRouteOptions) {
	path = NormalizePath(path)
	r = ResourceRouter(r, path)

	var opt CrudRouteOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	rec := path + KeyPath(handler.keyCols())
	r.Get(path, handler.List())
	r.Get(rec, handler.Get())

	if !opt.ReadOnly {
		r.Post(path, handler.Create())
		r.Put(rec, handler.Update())
		r.Patch(rec, handler.Update())
		r.Delete(rec, handler.Delete())

		r.Post(path+"/_batch", handler.BatchCreate())
		r.Patch(path+"/_batch", handler.BatchUpdate())
		r.Delete(path+"/_batch", handler.BatchDelete())
		if len(RoutableTransitions(handler.Entity.Config.States)) > 0 {
			r.Post(path+TransitionPath(handler.keyCols()), handler.Transition())
		}
	}

	r.Get(path+"/_events", handler.EventStream())

	// LLM-friendly documentation endpoint
	if !opt.NoLLMMD {
		registerLLMMDRoutes(r, handler, path, opt.ReadOnly)
	}
}

// ResourceRouter returns the router a CRUD mount at path (relative to r)
// registers on: r's prefix and middleware, with every route named by the
// mount's last path segment (router.ResourceGroup), the resource
// auth.RequireAPIScopes then scopes the route to. Routes that sit beside
// a mount's CRUD routes (EntityUI's _bulk, _export.csv, _pick) register
// through it too, so every subroute of a mount answers to its table and
// never to a guess from the path.
func ResourceRouter(r *router.Router, path string) *router.Router {
	return r.ResourceGroup(pathpkg.Base(r.Prefix() + NormalizePath(path)))
}

// NormalizePath strips trailing slashes from a path.
func NormalizePath(path string) string {
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// RegisterCrudRoutesFunc is a convenience that creates a CrudHandler and
// registers routes in one call.
func RegisterCrudRoutesFunc(r *router.Router, ent *entity.Entity, db DBExecutor, path string, opts ...CrudRouteOptions) *CrudHandler {
	handler := NewCrudHandler(ent, db)
	RegisterCrudRoutes(r, handler, path, opts...)
	return handler
}
