// Package kitx renames kit types for the app. An alias is a use of the
// type it names.
package kitx

import "github.com/DonaldMurillo/gofastr/core/middleware"

type Resp = middleware.IdempotentResponse
