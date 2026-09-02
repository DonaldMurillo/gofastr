// Package c is a third reflectset positive with a different layout: a
// table-driven applier using FieldByName through locals.
package c

import (
	"errors"
	"reflect"
)

var errNoField = errors.New("no such column")

// Row applier for a spreadsheet importer.
type applier struct {
	cols map[string]int
}

// applyColumn writes the parsed cell into the row struct. Pre-fix
// shape: the field value is located, then SetBool without CanSet.
func (a applier) applyColumn(row reflect.Value, col string, val bool) error {
	i, ok := a.cols[col]
	if !ok {
		return errNoField
	}
	fv := row.Field(i)
	fv.SetBool(val) // want `reflect SetBool on a field value without a CanSet check`
	return nil
}

// applyColumnFixed checks settability first.
func (a applier) applyColumnFixed(row reflect.Value, col string, val bool) error {
	i, ok := a.cols[col]
	if !ok {
		return errNoField
	}
	fv := row.Field(i)
	if !fv.CanSet() {
		return errors.New("column maps to an unexported field")
	}
	fv.SetBool(val)
	return nil
}
