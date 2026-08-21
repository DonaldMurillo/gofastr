package framework

import (
	"context"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Re-exports of framework/entity so existing callers, generated code, and
// example apps using framework.X keep compiling after the entity package
// extraction.

type (
	Entity                  = entity.Entity
	EntityConfig            = entity.EntityConfig
	ScopeConfig             = entity.ScopeConfig
	PaginationConfig        = entity.PaginationConfig
	ExposureConfig          = entity.ExposureConfig
	AccessControl           = entity.AccessControl
	Index                   = entity.Index
	Endpoint                = entity.Endpoint
	EntityDeclaration       = entity.EntityDeclaration
	ScopeDeclaration        = entity.ScopeDeclaration
	PaginationDeclaration   = entity.PaginationDeclaration
	ExposureDeclaration     = entity.ExposureDeclaration
	AccessDeclaration       = entity.AccessDeclaration
	ReadScopeConfig         = entity.ReadScopeConfig
	ReadScopeDeclaration    = entity.ReadScopeDeclaration
	RowPredicate            = entity.RowPredicate
	RowPredicateDeclaration = entity.RowPredicateDeclaration
	FieldDeclaration        = entity.FieldDeclaration
	Relation                = entity.Relation
	RelationType            = entity.RelationType
	Condition               = entity.Condition
	Order                   = entity.Order
	StringColumn            = entity.StringColumn
	IntColumn               = entity.IntColumn
	FloatColumn             = entity.FloatColumn
	BoolColumn              = entity.BoolColumn
	TimestampColumn         = entity.TimestampColumn
	UUIDColumn              = entity.UUIDColumn
	ValidatorFunc           = entity.ValidatorFunc
	ValidationRegistry      = entity.ValidationRegistry
)

const (
	RelHasOne     = entity.RelHasOne
	RelHasMany    = entity.RelHasMany
	RelManyToOne  = entity.RelManyToOne
	RelManyToMany = entity.RelManyToMany
)

// Define wraps entity.Define.
func Define(name string, config entity.EntityConfig) *entity.Entity {
	return entity.Define(name, config)
}

// HasOne wraps entity.HasOne.
func HasOne(name string, ent string, foreignKey string) entity.Relation {
	return entity.HasOne(name, ent, foreignKey)
}

// HasMany wraps entity.HasMany.
func HasMany(name string, ent string, foreignKey string) entity.Relation {
	return entity.HasMany(name, ent, foreignKey)
}

// BelongsTo wraps entity.BelongsTo.
func BelongsTo(name string, ent string, foreignKey string) entity.Relation {
	return entity.BelongsTo(name, ent, foreignKey)
}

// ManyToMany wraps entity.ManyToMany.
func ManyToMany(name string, ent string, throughTable string, sourceFK string, targetFK string) entity.Relation {
	return entity.ManyToMany(name, ent, throughTable, sourceFK, targetFK)
}

// NewStringColumn wraps entity.NewStringColumn.
func NewStringColumn(name string) entity.StringColumn { return entity.NewStringColumn(name) }

// NewIntColumn wraps entity.NewIntColumn.
func NewIntColumn(name string) entity.IntColumn { return entity.NewIntColumn(name) }

// NewFloatColumn wraps entity.NewFloatColumn.
func NewFloatColumn(name string) entity.FloatColumn { return entity.NewFloatColumn(name) }

// NewBoolColumn wraps entity.NewBoolColumn.
func NewBoolColumn(name string) entity.BoolColumn { return entity.NewBoolColumn(name) }

// NewTimestampColumn wraps entity.NewTimestampColumn.
func NewTimestampColumn(name string) entity.TimestampColumn { return entity.NewTimestampColumn(name) }

// NewUUIDColumn wraps entity.NewUUIDColumn.
func NewUUIDColumn(name string) entity.UUIDColumn { return entity.NewUUIDColumn(name) }

// NewValidationRegistry wraps entity.NewValidationRegistry.
func NewValidationRegistry() *entity.ValidationRegistry { return entity.NewValidationRegistry() }

// Required wraps entity.Required.
func Required(fields ...string) entity.ValidatorFunc { return entity.Required(fields...) }

// Unique wraps entity.Unique.
func Unique(field string, checkFn func(ctx context.Context, value any) bool) entity.ValidatorFunc {
	return entity.Unique(field, checkFn)
}

// Custom wraps entity.Custom.
func Custom(name string, fn func(ctx context.Context, data map[string]any) map[string]string) entity.ValidatorFunc {
	return entity.Custom(name, fn)
}

// FormatValidationErrors wraps entity.FormatValidationErrors.
func FormatValidationErrors(errors map[string]string) []string {
	return entity.FormatValidationErrors(errors)
}

// And wraps entity.And.
func And(conds ...entity.Condition) entity.Condition { return entity.And(conds...) }

// Or wraps entity.Or.
func Or(conds ...entity.Condition) entity.Condition { return entity.Or(conds...) }

// Not wraps entity.Not.
func Not(c entity.Condition) entity.Condition { return entity.Not(c) }
