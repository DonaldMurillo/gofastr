package framework

import (
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/framework/entity"
)

// RegisterCrudRoutes registers the standard CRUD routes plus batch endpoints
// on the given router.
//
//	GET    /path             → List
//	GET    /path/{id}        → Get
//	POST   /path             → Create
//	PUT    /path/{id}        → Update
//	DELETE /path/{id}        → Delete
//	POST   /path/_batch      → BatchCreate (atomic; all-or-nothing)
//	PATCH  /path/_batch      → BatchUpdate (atomic; all-or-nothing)
//	DELETE /path/_batch      → BatchDelete (atomic; all-or-nothing)
//
// The batch routes use a "_batch" segment, which Go 1.22's ServeMux ranks
// above the wildcard /{id} so they take precedence over Get/Update/Delete
// on the same prefix.
func RegisterCrudRoutes(r *router.Router, handler *CrudHandler, path string) {
	path = normalizePath(path)

	r.Get(path, handler.List())
	r.Get(path+"/{id}", handler.Get())
	r.Post(path, handler.Create())
	r.Put(path+"/{id}", handler.Update())
	r.Delete(path+"/{id}", handler.Delete())

	r.Post(path+"/_batch", handler.BatchCreate())
	r.Patch(path+"/_batch", handler.BatchUpdate())
	r.Delete(path+"/_batch", handler.BatchDelete())

	r.Get(path+"/_events", handler.EventStream())
}

// normalizePath strips trailing slashes from a path.
func normalizePath(path string) string {
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// RegisterCrudRoutesFunc is a convenience that creates a CrudHandler and
// registers routes in one call.
func RegisterCrudRoutesFunc(r *router.Router, ent *entity.Entity, db DBExecutor, path string) *CrudHandler {
	handler := NewCrudHandler(ent, db)
	RegisterCrudRoutes(r, handler, path)
	return handler
}
