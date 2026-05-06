package framework

import (
	"github.com/gofastr/gofastr/core/router"
)

// RegisterCrudRoutes registers all 5 CRUD routes on the given router.
//   - GET    /path          → List
//   - GET    /path/{id}     → Get
//   - POST   /path          → Create
//   - PUT    /path/{id}     → Update
//   - DELETE /path/{id}     → Delete
func RegisterCrudRoutes(r *router.Router, handler *CrudHandler, path string) {
	// Normalize path: ensure no trailing slash
	path = normalizePath(path)

	r.Get(path, handler.List())
	r.Get(path+"/{id}", handler.Get())
	r.Post(path, handler.Create())
	r.Put(path+"/{id}", handler.Update())
	r.Delete(path+"/{id}", handler.Delete())
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
func RegisterCrudRoutesFunc(r *router.Router, entity *Entity, db DBExecutor, path string) *CrudHandler {
	handler := NewCrudHandler(entity, db)
	RegisterCrudRoutes(r, handler, path)
	return handler
}
