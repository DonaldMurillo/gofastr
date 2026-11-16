package framework

import (
	"context"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Root spellings of framework/entity.

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
	DisplayConfig           = entity.DisplayConfig
	EntityNav               = entity.EntityNav
	ListView                = entity.ListView
	EntityForm              = entity.EntityForm
	FormItem                = entity.FormItem
	CardFields              = entity.CardFields
	FieldDisplay            = entity.FieldDisplay
	StatesConfig            = entity.StatesConfig
	Transition              = entity.Transition
	Relation                = entity.Relation
	RelationType            = entity.RelationType
	OnDeleteAction          = entity.OnDeleteAction
	Condition               = entity.Condition
	Order                   = entity.Order
	StringColumn            = entity.StringColumn
	EnumColumn[T ~string]   = entity.EnumColumn[T]
	IntColumn               = entity.IntColumn
	FloatColumn             = entity.FloatColumn
	BoolColumn              = entity.BoolColumn
	TimestampColumn         = entity.TimestampColumn
	UUIDColumn              = entity.UUIDColumn
	ValidatorFunc           = entity.ValidatorFunc
	ValidationRegistry      = entity.ValidationRegistry
)

const (
	Anyone = entity.Anyone

	RelHasOne     = entity.RelHasOne
	RelHasMany    = entity.RelHasMany
	RelManyToOne  = entity.RelManyToOne
	RelManyToMany = entity.RelManyToMany

	OnDeleteNoAction = entity.OnDeleteNoAction
	OnDeleteRestrict = entity.OnDeleteRestrict
	OnDeleteCascade  = entity.OnDeleteCascade
	OnDeleteSetNull  = entity.OnDeleteSetNull
)

func Define(name string, config entity.EntityConfig) *entity.Entity {
	return entity.Define(name, config)
}

func HasOne(name string, ent string, foreignKey string) entity.Relation {
	return entity.HasOne(name, ent, foreignKey)
}

func HasMany(name string, ent string, foreignKey string) entity.Relation {
	return entity.HasMany(name, ent, foreignKey)
}

func BelongsTo(name string, ent string, foreignKey string) entity.Relation {
	return entity.BelongsTo(name, ent, foreignKey)
}

func ManyToMany(name string, ent string, throughTable string, sourceFK string, targetFK string) entity.Relation {
	return entity.ManyToMany(name, ent, throughTable, sourceFK, targetFK)
}

func NewStringColumn(name string) entity.StringColumn { return entity.NewStringColumn(name) }

func NewEnumColumn[T ~string](name string) entity.EnumColumn[T] { return entity.NewEnumColumn[T](name) }

func NewIntColumn(name string) entity.IntColumn { return entity.NewIntColumn(name) }

func NewFloatColumn(name string) entity.FloatColumn { return entity.NewFloatColumn(name) }

func NewBoolColumn(name string) entity.BoolColumn { return entity.NewBoolColumn(name) }

func NewTimestampColumn(name string) entity.TimestampColumn { return entity.NewTimestampColumn(name) }

func NewUUIDColumn(name string) entity.UUIDColumn { return entity.NewUUIDColumn(name) }

func NewValidationRegistry() *entity.ValidationRegistry { return entity.NewValidationRegistry() }

func Required(fields ...string) entity.ValidatorFunc { return entity.Required(fields...) }

func Unique(field string, checkFn func(ctx context.Context, value any) bool) entity.ValidatorFunc {
	return entity.Unique(field, checkFn)
}

func Custom(name string, fn func(ctx context.Context, data map[string]any) map[string]string) entity.ValidatorFunc {
	return entity.Custom(name, fn)
}

func FormatValidationErrors(errors map[string]string) []string {
	return entity.FormatValidationErrors(errors)
}

func And(conds ...entity.Condition) entity.Condition { return entity.And(conds...) }

func Or(conds ...entity.Condition) entity.Condition { return entity.Or(conds...) }

func Not(c entity.Condition) entity.Condition { return entity.Not(c) }

func JoinKey(parts []string) string { return entity.JoinKey(parts) }

func RowKey(row map[string]any, cols []string, names func(string) string) (string, bool) {
	return entity.RowKey(row, cols, names)
}
