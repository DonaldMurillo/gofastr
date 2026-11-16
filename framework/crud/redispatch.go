package crud

import (
	"context"
	"net/http"
)

// Redispatch serves method and path through router as the caller on ctx
// (mcp.WithRequest), the full middleware chain included, and returns the
// decoded JSON body. A status of 400 or more is returned as an error.
func Redispatch(ctx context.Context, router http.Handler, method, path string, body any) (any, error) {
	return runToolRequest(ctx, router, method, path, body)
}
