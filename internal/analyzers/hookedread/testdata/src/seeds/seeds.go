// Package seeds is an in-process Go caller outside the screen packages:
// reading stored values is its job (a seed lookup, read-modify-write).
package seeds

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/crud"
)

func resolve(ctx context.Context, ch *crud.CrudHandler) {
	_, _ = ch.ListAll(ctx, crud.ListOptions{Limit: 1})
	_, _ = ch.GetOne(ctx, "x", nil)
}
