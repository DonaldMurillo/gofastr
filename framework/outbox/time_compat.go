package outbox

import (
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// outboxTimePtr decodes a nullable time column: SQL NULL stays nil, anything
// else decodes via query.ParseDBTime (the shared parser that replaced this
// package's own outboxTime/parseOutboxTime and battery/queue's twin).
func outboxTimePtr(src any) (*time.Time, error) {
	if src == nil {
		return nil, nil
	}
	value, err := query.ParseDBTime(src)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
