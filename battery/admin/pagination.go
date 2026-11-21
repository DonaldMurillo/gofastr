package admin

import (
	"context"
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	pageParam   = "p"
	opsMaxLimit = 1000
)

func decodePage(q url.Values, defaultLimit, maxLimit int) (page, limit int) {
	page, limit = 1, defaultLimit
	if n, err := strconv.Atoi(q.Get(pageParam)); err == nil && n > 1 {
		page = n
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, maxLimit)
	}
	return page, limit
}

func pageCount(total, limit int) int {
	if limit <= 0 || total <= 0 {
		return 1
	}
	return (total-1)/limit + 1
}

func pagePager(ctx context.Context, path string, carry url.Values, page, pages int) *ui.PaginationConfig {
	if pages <= 1 {
		return nil
	}
	return &ui.PaginationConfig{
		Page: page, Pages: pages, Path: path, Query: carry, PageParam: pageParam, Ctx: ctx,
	}
}
