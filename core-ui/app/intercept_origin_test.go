package app

import "testing"

func TestInterceptNormalizesOrigins(t *testing.T) {
	for _, test := range []struct{ pattern, path string }{
		{"/orders/{id}", "/orders/2"},
		{"/orders/:id", "/orders/3"},
		{"/orders/{id:int}", "/orders/4"},
		{"/docs/{path...}", "/docs/setup/install"},
	} {
		for _, also := range []bool{false, true} {
			t.Run(test.pattern+map[bool]string{false: "/from", true: "/also"}[also], func(t *testing.T) {
				application := NewApp("intercept")
				application.Register(test.pattern, &ixDetail{}, nil)
				application.Register("/known", &ixList{}, nil)
				option := InterceptFrom(test.pattern, ScreenDrawer)
				if also {
					option = InterceptFrom("/known", ScreenDrawer, test.pattern)
				}
				application.Register("/panel", &ixList{}, nil, option)
				application.Router.ValidateIntercepts()
				if _, ok := application.Router.InterceptFor("/panel", test.path); !ok {
					t.Fatalf("origin %q did not match %q", test.path, test.pattern)
				}
			})
		}
	}
}
