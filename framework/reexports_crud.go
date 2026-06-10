package framework

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/db"
)

// Re-exports of framework/crud + framework/db so existing callers, generated
// code, and external apps using framework.X keep compiling after the crud
// package extraction.

type (
	CrudHandler  = crud.CrudHandler
	ListResponse = crud.ListResponse
	ListOptions  = crud.ListOptions
	JSONCase     = crud.JSONCase
	IncludeNode  = crud.IncludeNode
	DBExecutor   = db.Executor
)

const (
	CaseCamel          = crud.CaseCamel
	CaseSnake          = crud.CaseSnake
	MaxBatchSize       = crud.MaxBatchSize
	MaxMultipartMemory = crud.MaxMultipartMemory
)

var (
	NewCrudHandler         = crud.NewCrudHandler
	RegisterCrudRoutes     = crud.RegisterCrudRoutes
	RegisterCrudRoutesFunc = crud.RegisterCrudRoutesFunc
	MarshalEntity          = crud.MarshalEntity
	UnmarshalEntity        = crud.UnmarshalEntity
	IsNotFound             = crud.IsNotFound
	EagerLoad              = crud.EagerLoad
	RegisterEntityMCPTools = crud.RegisterEntityMCPTools
)

// TypedQuery and NewTypedQuery are generics — declared as wrappers since Go
// generic type aliases / generic var bindings are recent.

type TypedQuery[T any] = crud.TypedQuery[T]

func NewTypedQuery[T any](h *crud.CrudHandler) *crud.TypedQuery[T] {
	return crud.NewTypedQuery[T](h)
}
