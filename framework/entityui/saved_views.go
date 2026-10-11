package entityui

import (
	"context"
	"errors"
)

// SavedView is one user's named list state for one entity: the filter
// text and the columns, in order. The name is a label only: it is never
// looked up in a catalog and never collides with a declared view's key.
type SavedView struct {
	ID     string
	Entity string
	Name   string
	// Filter is the list's DSL filter text. It is re-parsed and re-checked
	// against the entity's fields every time the view opens.
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
// ctx only: the store reads the owner (handler.GetUser's GetID) and the
// tenant from ctx, never from an argument or the view, and a view another
// owner or tenant made is ErrSavedViewNotFound, the same answer as an id
// that does not exist. A ctx with no user is refused. battery/admin
// provides the table; UI.WithSavedViews hands it to the list.
type SavedViewStore interface {
	// List returns the caller's views of entity, by name.
	List(ctx context.Context, entity string) ([]SavedView, error)
	// Get returns one of the caller's views of entity.
	Get(ctx context.Context, entity, id string) (SavedView, error)
	// Create stores v for the caller and returns it with its ID. It
	// refuses a blank name (ErrSavedViewBlank), a name the caller already
	// uses for entity (ErrSavedViewExists), a view past SavedViewCap
	// (ErrSavedViewCap) and a name, filter or column list past its limit
	// (ErrSavedViewTooLong).
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
