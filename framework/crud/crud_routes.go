package crud

import (
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// registerLLMMDRoutes registers the /{path}/llm.md documentation endpoint for
// a single entity. Called automatically by RegisterCrudRoutes.
func registerLLMMDRoutes(r *router.Router, ent *entity.Entity, path string) {
	path = NormalizePath(path)
	r.Get(path+"/llm.md", LLMMDHandler(ent))
}

// CrudRouteOptions controls which routes are registered by RegisterCrudRoutes.
type CrudRouteOptions struct {
	NoLLMMD  bool // disable auto-generated /path/llm.md
	ReadOnly bool // register only the read routes (List/Get/events) — for views and other read-only objects
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
//
// The batch routes use a "_batch" segment, which Go 1.22's ServeMux ranks
// above the wildcard /{id} so they take precedence over Get/Update/Delete
// on the same prefix.
func RegisterCrudRoutes(r *router.Router, handler *CrudHandler, path string, opts ...CrudRouteOptions) {
	path = NormalizePath(path)

	var opt CrudRouteOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	r.Get(path, handler.List())
	r.Get(path+"/{id}", handler.Get())

	if !opt.ReadOnly {
		r.Post(path, handler.Create())
		r.Put(path+"/{id}", handler.Update())
		r.Patch(path+"/{id}", handler.Update())
		r.Delete(path+"/{id}", handler.Delete())

		r.Post(path+"/_batch", handler.BatchCreate())
		r.Patch(path+"/_batch", handler.BatchUpdate())
		r.Delete(path+"/_batch", handler.BatchDelete())
	}

	r.Get(path+"/_events", handler.EventStream())

	// LLM-friendly documentation endpoint
	if !opt.NoLLMMD {
		registerLLMMDRoutes(r, handler.Entity, path)
	}
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
