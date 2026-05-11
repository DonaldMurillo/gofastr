package framework

import "github.com/gofastr/gofastr/framework/openapi"

// Re-exports of framework/openapi so existing callers (kiln/live, framework
// tests) using framework.X keep compiling after the openapi extraction.

var EntityOpenAPI = openapi.EntityOpenAPI
