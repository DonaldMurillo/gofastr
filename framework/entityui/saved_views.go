package entityui

import (
	"context"
	"errors"
)

// SavedView is one user's named list state for one entity: the filter
// text and the columns. The name is a label only.
type SavedView struct {
	ID     string
	Entity string
	Name   string
	// Filter is the list's DSL filter text, re-checked every time the view opens.
	Filter string
	// Columns are the shown columns in order; empty means the list's own.
	Columns []string
}

// Limits a SavedViewStore enforces on every Create.
const (
	// SavedViewCap is the most views one owner keeps per entity.
	SavedViewCap = 50
	// SavedViewNameMax is the longest name, in bytes.
	SavedViewNameMax = 80
	// SavedViewFilterMax is the longest filter text, in bytes.
	SavedViewFilterMax = 2000
	// SavedViewColumnsMax is the most columns a view names.
	SavedViewColumnsMax = 50
)

// SavedViewStore keeps saved views. Every method acts for the caller in
// ctx only, reading owner and tenant from ctx; another owner's view is
// ErrSavedViewNotFound, and a ctx with no user is refused.
type SavedViewStore interface {
	// List returns the caller's views of entity, by name.
	List(ctx context.Context, entity string) ([]SavedView, error)
	// Get returns one of the caller's views of entity.
	Get(ctx context.Context, entity, id string) (SavedView, error)
	// Create stores v for the caller and returns it with its ID, refusing
	// with ErrSavedViewBlank, ErrSavedViewExists, ErrSavedViewCap or ErrSavedViewTooLong.
	Create(ctx context.Context, v SavedView) (SavedView, error)
	// Delete removes one of the caller's views of entity.
	Delete(ctx context.Context, entity, id string) error
}

// SavedViewStore errors.
var (
	ErrSavedViewNotFound = errors.New("entityui: saved view not found")
	ErrSavedViewBlank    = errors.New("entityui: a saved view needs a name")
	ErrSavedViewExists   = errors.New("entityui: a saved view with that name exists")
	ErrSavedViewCap      = errors.New("entityui: too many saved views")
	ErrSavedViewTooLong  = errors.New("entityui: saved view name, filter or columns too long")
)
