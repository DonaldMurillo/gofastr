//go:build e2e_real

package harness

import (
	"encoding/json"
	"errors"
)

// errorsAsImpl is a thin wrapper so the main e2e_plumbing_test.go
// file doesn't need to import "errors" directly.
func errorsAsImpl(err error, target any) bool { return errors.As(err, target) }

// keep json import honest for places where the test file may have
// removed its last use during edits.
var _ = json.Marshal
