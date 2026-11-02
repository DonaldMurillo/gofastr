package framework

import (
	coreopenapi "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// Re-exports of framework/openapi so existing callers using framework.X
// keep compiling after the openapi extraction.

// EntityOpenAPI wraps openapi.EntityOpenAPI with the Exposure-only CRUD
// check (crudMounted=nil). App.Start passes its route predicate instead,
// so the served /openapi.json never advertises CRUD paths registration
// did not mount (#266); direct callers of this wrapper document per
// Exposure, the historical behavior, and name every field in camelCase
// under no API prefix. A host serving an App's routes wants
// App.OpenAPISpec, which follows the app's JSONCase and APIPrefix.
func EntityOpenAPI(registry entity.Registry, title string, version string, basePath ...string) *coreopenapi.Spec {
	return openapi.EntityOpenAPI(registry, title, version, nil, basePath...)
}
