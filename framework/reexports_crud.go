package framework

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"net/http"
)

// Re-exports of framework/crud + framework/db so existing callers, generated
// code, and external apps using framework.X keep compiling after the crud
// package extraction.

type (
	CrudHandler     = crud.CrudHandler
	ListResponse    = crud.ListResponse
	ListOptions     = crud.ListOptions
	JSONCase        = crud.JSONCase
	IncludeNode     = crud.IncludeNode
	ValidationError = crud.ValidationError
	DBExecutor      = db.Executor
)

const (
	CaseCamel          = crud.CaseCamel
	CaseSnake          = crud.CaseSnake
	MaxBatchSize       = crud.MaxBatchSize
	MaxMultipartMemory = crud.MaxMultipartMemory
)

// NewCrudHandler wraps crud.NewCrudHandler.
func NewCrudHandler(ent *entity.Entity, db crud.DBExecutor) *crud.CrudHandler {
	return crud.NewCrudHandler(ent, db)
}

// RegisterCrudRoutes wraps crud.RegisterCrudRoutes.
func RegisterCrudRoutes(r *router.Router, handler *crud.CrudHandler, path string, opts ...crud.CrudRouteOptions) {
	crud.RegisterCrudRoutes(r, handler, path, opts...)
}

// RegisterCrudRoutesFunc wraps crud.RegisterCrudRoutesFunc.
func RegisterCrudRoutesFunc(r *router.Router, ent *entity.Entity, db crud.DBExecutor, path string, opts ...crud.CrudRouteOptions) *crud.CrudHandler {
	return crud.RegisterCrudRoutesFunc(r, ent, db, path, opts...)
}

// MarshalEntity wraps crud.MarshalEntity.
func MarshalEntity(src any) (map[string]any, error) { return crud.MarshalEntity(src) }

// UnmarshalEntity wraps crud.UnmarshalEntity.
func UnmarshalEntity(m map[string]any, dest any) error { return crud.UnmarshalEntity(m, dest) }

// IsNotFound wraps crud.IsNotFound.
func IsNotFound(err error) bool { return crud.IsNotFound(err) }

// EagerLoad wraps crud.EagerLoad.
func EagerLoad(ctx context.Context, db crud.DBExecutor, ent *entity.Entity, relations []entity.Relation, ids []string, reg ...entity.Registry) (map[string]map[string]any, error) {
	return crud.EagerLoad(ctx, db, ent, relations, ids, reg...)
}

// RegisterEntityMCPTools wraps crud.RegisterEntityMCPTools.
func RegisterEntityMCPTools(server *mcp.Server, crud1 *crud.CrudHandler, router http.Handler) error {
	return crud.RegisterEntityMCPTools(server, crud1, router)
}

// WithServerWrites wraps crud.WithServerWrites.
func WithServerWrites(ctx context.Context) context.Context { return crud.WithServerWrites(ctx) }

// WithReadHooks wraps crud.WithReadHooks.
func WithReadHooks(ctx context.Context) context.Context { return crud.WithReadHooks(ctx) }

// NewValidationError wraps crud.NewValidationError.
func NewValidationError(fields map[string][]string) *crud.ValidationError {
	return crud.NewValidationError(fields)
}

// TypedQuery and NewTypedQuery are generics — declared as wrappers since Go
// generic type aliases / generic var bindings are recent.

type TypedQuery[T any] = crud.TypedQuery[T]

func NewTypedQuery[T any](h *crud.CrudHandler) *crud.TypedQuery[T] {
	return crud.NewTypedQuery[T](h)
}
