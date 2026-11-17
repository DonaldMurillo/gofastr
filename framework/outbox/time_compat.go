package outbox

import (
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

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
