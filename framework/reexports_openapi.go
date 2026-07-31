package framework

import (
	coreopenapi "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// Re-exports of framework/openapi so existing callers (kiln/live, framework
// tests) using framework.X keep compiling after the openapi extraction.

// EntityOpenAPI wraps openapi.EntityOpenAPI.
func EntityOpenAPI(registry entity.Registry, title string, version string, basePath ...string) *coreopenapi.Spec {
	return openapi.EntityOpenAPI(registry, title, version, basePath...)
}
