package ui

import (
	"strconv"
	"sync/atomic"
)

// autoIDCounter provides unique IDs for UI components.
var autoIDCounter int64

// autoID generates a unique ID with the given prefix.
func autoID(prefix string) string {
	n := atomic.AddInt64(&autoIDCounter, 1)
	return prefix + "-" + strconv.FormatInt(n, 36)
}
