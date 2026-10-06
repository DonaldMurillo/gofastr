package entityui

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// render draws the record or create screen. W2 implements it.
func (b *RecordBuilder) render(ctx context.Context) (render.HTML, error) {
	return "", errors.New("entityui: record not implemented")
}
