package entityui

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// render draws the list. W1 implements it.
func (b *ListBuilder) render(ctx context.Context) (render.HTML, error) {
	return "", errors.New("entityui: list not implemented")
}
