// Package a's errors.
package a

import "errors"

var errBadLimit = errors.New("limit must be at least 1")
