package router_test

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// router.Middleware must be an alias of middleware.Middleware so
// battery/auth's middleware flows into router.Use without a cast.
var (
	_ router.Middleware                              = middleware.Middleware(nil)
	_ middleware.Middleware                          = router.Middleware(nil)
	_ func(*router.Router, ...middleware.Middleware) = (*router.Router).Use
)
