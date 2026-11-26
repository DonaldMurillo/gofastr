package crud

import (
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func registerLLMMDRoutes(r *router.Router, ch *CrudHandler, path string, readOnly bool) {
	path = NormalizePath(path)
	opts := LLMMDOptions{ReadOnly: readOnly}
	if ch.BasePath != "" {
		opts.Path = ch.mcpBase()
	}
	r.Get(path+"/llm.md", LLMMDHandlerFor(ch, opts))
}

// CrudRouteOptions controls which routes are registered by RegisterCrudRoutes.
type CrudRouteOptions struct {
	// NoLLMMD skips the /path/llm.md route.
	NoLLMMD bool
	// ReadOnly registers only List, Get and _events.
	ReadOnly bool
	// States is read by CrudRoutePatterns only: a non-system move adds the transition route.
	States *entity.StatesConfig
	// Key is read by CrudRoutePatterns only, to spell the record routes; nil means "id".
	Key []string
}

// KeyPath is the route tail naming one record keyed on cols: "/{id}" for
// one column, else one wildcard per column ("/{student_id}/{course_id}").
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

// TransitionPath is KeyPath(cols) then /transitions/{key}, the wildcard
// renamed when a key column is named "key".
func TransitionPath(cols []string) string {
	return KeyPath(cols) + "/transitions/{" + transitionParam(cols) + "}"
}

func transitionParam(cols []string) string {
	p := "key"
	for len(cols) > 1 && slices.Contains(cols, p) {
		p += "_"
	}
	return p
}

// PathRecordID is the record id a request routed through KeyPath(cols)
// names, as entity.JoinKey renders it. It is "" when a key segment is
// empty or not storable text (invalid UTF-8 or a NUL).
func PathRecordID(r *http.Request, cols []string) string {
	if len(cols) <= 1 {
		id := r.PathValue("id")
		if !textsafe.Storable(id) {
			return ""
		}
		return id
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		v := r.PathValue(c)
		if v == "" || !textsafe.Storable(v) {
			return ""
		}
		parts[i] = v
	}
	return entity.JoinKey(parts)
}

func (ch *CrudHandler) pathID(r *http.Request) string {
	return PathRecordID(r, ch.keyCols())
}

// CrudRoutePatterns returns the "METHOD /pattern" set RegisterCrudRoutes
// mounts for path, without mounting anything.
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

// RegisterCrudRoutes registers on r, at path: GET List, GET Get, POST
// Create, PUT and PATCH Update, DELETE Delete, the atomic _batch POST,
// PATCH and DELETE, POST <record>/transitions/{key} when the entity has a
// non-system move, GET _events and GET llm.md. The record routes take one
// segment per key column (KeyPath).
func RegisterCrudRoutes(r *router.Router, handler *CrudHandler, path string, opts ...CrudRouteOptions) {
	path = NormalizePath(path)
	r = EntityRouter(r, handler.Entity)
	logControlNamedFields(handler.Entity)

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

	if !opt.NoLLMMD {
		registerLLMMDRoutes(r, handler, path, opt.ReadOnly)
	}
}

// EntityRouter returns r with every route named by the entity's table
// (router.ResourceGroup), the resource auth.RequireAPIScopes scopes it to.
func EntityRouter(r *router.Router, ent *entity.Entity) *router.Router {
	return r.ResourceGroup(ent.GetTable())
}

// NormalizePath strips trailing slashes from a path.
func NormalizePath(path string) string {
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// RegisterCrudRoutesFunc creates a CrudHandler and registers its routes.
func RegisterCrudRoutesFunc(r *router.Router, ent *entity.Entity, db DBExecutor, path string, opts ...CrudRouteOptions) *CrudHandler {
	handler := NewCrudHandler(ent, db)
	RegisterCrudRoutes(r, handler, path, opts...)
	return handler
}
