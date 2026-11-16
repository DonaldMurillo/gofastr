package framework

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Root spellings of framework/crud and framework/db.

type (
	CrudHandler     = crud.CrudHandler
	ListResponse    = crud.ListResponse
	ListOptions     = crud.ListOptions
	NestedFilter    = crud.NestedFilter
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

func NewCrudHandler(ent *entity.Entity, db crud.DBExecutor) *crud.CrudHandler {
	return crud.NewCrudHandler(ent, db)
}

func RegisterCrudRoutes(r *router.Router, handler *crud.CrudHandler, path string, opts ...crud.CrudRouteOptions) {
	crud.RegisterCrudRoutes(r, handler, path, opts...)
}

func RegisterCrudRoutesFunc(r *router.Router, ent *entity.Entity, db crud.DBExecutor, path string, opts ...crud.CrudRouteOptions) *crud.CrudHandler {
	return crud.RegisterCrudRoutesFunc(r, ent, db, path, opts...)
}

func MarshalEntity(src any) (map[string]any, error) { return crud.MarshalEntity(src) }

func UnmarshalEntity(m map[string]any, dest any) error { return crud.UnmarshalEntity(m, dest) }

func IsNotFound(err error) bool { return crud.IsNotFound(err) }

func EagerLoad(ctx context.Context, db crud.DBExecutor, ent *entity.Entity, relations []entity.Relation, ids []string, reg ...entity.Registry) (map[string]map[string]any, error) {
	return crud.EagerLoad(ctx, db, ent, relations, ids, reg...)
}

func RegisterEntityMCPTools(server *mcp.Server, crud1 *crud.CrudHandler, router http.Handler) error {
	return crud.RegisterEntityMCPTools(server, crud1, router)
}

func WithServerWrites(ctx context.Context) context.Context { return crud.WithServerWrites(ctx) }

func WithStateOverride(ctx context.Context, reason string) context.Context {
	return crud.WithStateOverride(ctx, reason)
}

func TransitionFromContext(ctx context.Context) string { return crud.TransitionFromContext(ctx) }

func WithUploadedKeys(ctx context.Context, keys ...string) context.Context {
	return crud.WithUploadedKeys(ctx, keys...)
}

func WithReadHooks(ctx context.Context) context.Context { return crud.WithReadHooks(ctx) }

func NewValidationError(fields map[string][]schema.Violation) *crud.ValidationError {
	return crud.NewValidationError(fields)
}

type TypedQuery[T any] = crud.TypedQuery[T]

func NewTypedQuery[T any](h *crud.CrudHandler) *crud.TypedQuery[T] {
	return crud.NewTypedQuery[T](h)
}
