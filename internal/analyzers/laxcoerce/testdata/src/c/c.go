// Package c is a third laxcoerce positive with a different layout: the
// map is a named type used as a method receiver, and the !ok branch is
// the else of a bare `if ok`.
package c

import "errors"

var errNotATable = errors.New("not a table")

// Table is a decoded JSON object.
type Table map[string]any

// Caption returns the table's caption. A caption present as a number
// falls into the else branch and reads as "no caption".
func (t Table) Caption() (string, bool, error) {
	if s, ok := t["caption"].(string); ok { // want `type assertion on t treated as absence`
		return s, true, nil
	} else {
		return "", false, nil
	}
}

// CaptionFixed is the fix posture on the same receiver.
func (t Table) CaptionFixed() (string, bool, error) {
	v, present := t["caption"]
	if !present {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, errNotATable
	}
	return s, true, nil
}
