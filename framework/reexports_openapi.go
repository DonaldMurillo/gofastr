package framework

import (
	coreopenapi "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

// EntityOpenAPI documents every entity its Exposure allows, in camelCase
// and under no API prefix. For the spec an App serves, use App.OpenAPISpec.
func EntityOpenAPI(registry entity.Registry, title string, version string, basePath ...string) *coreopenapi.Spec {
	return openapi.EntityOpenAPI(registry, title, version, nil, basePath...)
}
