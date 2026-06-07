package framework

import "github.com/DonaldMurillo/gofastr/framework/entity"

// Re-exports of framework/entity so existing callers, generated code, and
// example apps using framework.X keep compiling after the entity package
// extraction.

type (
	Entity             = entity.Entity
	EntityConfig       = entity.EntityConfig
	AccessControl      = entity.AccessControl
	Index              = entity.Index
	Endpoint           = entity.Endpoint
	EntityDeclaration  = entity.EntityDeclaration
	FieldDeclaration   = entity.FieldDeclaration
	Relation           = entity.Relation
	RelationType       = entity.RelationType
	Condition          = entity.Condition
	Order              = entity.Order
	StringColumn       = entity.StringColumn
	IntColumn          = entity.IntColumn
	FloatColumn        = entity.FloatColumn
	BoolColumn         = entity.BoolColumn
	TimestampColumn    = entity.TimestampColumn
	UUIDColumn         = entity.UUIDColumn
	ValidatorFunc      = entity.ValidatorFunc
	ValidationRegistry = entity.ValidationRegistry
)

const (
	RelHasOne     = entity.RelHasOne
	RelHasMany    = entity.RelHasMany
	RelManyToOne  = entity.RelManyToOne
	RelManyToMany = entity.RelManyToMany
)

var (
	Define                 = entity.Define
	LoadEntityDeclaration  = entity.LoadEntityDeclaration
	LoadEntityDeclarations = entity.LoadEntityDeclarations
	HasOne                 = entity.HasOne
	HasMany                = entity.HasMany
	BelongsTo              = entity.BelongsTo
	ManyToMany             = entity.ManyToMany
	NewStringColumn        = entity.NewStringColumn
	NewIntColumn           = entity.NewIntColumn
	NewFloatColumn         = entity.NewFloatColumn
	NewBoolColumn          = entity.NewBoolColumn
	NewTimestampColumn     = entity.NewTimestampColumn
	NewUUIDColumn          = entity.NewUUIDColumn
	NewValidationRegistry  = entity.NewValidationRegistry
	Required               = entity.Required
	Unique                 = entity.Unique
	Custom                 = entity.Custom
	FormatValidationErrors = entity.FormatValidationErrors
	And                    = entity.And
	Or                     = entity.Or
	Not                    = entity.Not
)
