package app

import "testing"

func TestRoutePathKeepsEscapedSlash(t *testing.T) {
	for escaped, want := range map[string]string{
		"/orders/a%2Fb":       "/orders/a%2Fb",
		"/orders/a%2fb":       "/orders/a%2Fb",
		"/caf%C3%A9":          "/café",
		"/orders/50%25":       "/orders/50%25",
		"/orders/a%252Fb":     "/orders/a%252Fb",
		"/plain/path":         "/plain/path",
		"/orders/bad%zz":      "/orders/bad%25zz",
		"/orders/s%201/c%2F2": "/orders/s 1/c%2F2",
	} {
		if got := RoutePath(escaped); got != want {
			t.Errorf("RoutePath(%q) = %q, want %q", escaped, got, want)
		}
	}
}

func TestEscapeRoutePathInvertsRoutePath(t *testing.T) {
	for _, escaped := range []string{
		"/orders/a%2Fb", "/caf%C3%A9", "/orders/50%25", "/orders/a%252Fb", "/plain/path", "/",
	} {
		if got := EscapeRoutePath(RoutePath(escaped)); got != escaped {
			t.Errorf("EscapeRoutePath(RoutePath(%q)) = %q", escaped, got)
		}
	}
}

func TestResolveKeyWithSlash(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/a/b", &catchScreen{}), nil)
	r.Screen(NewScreen("/orders/:id", &catchScreen{}), nil)
	r.Screen(NewScreen("/enrollments/:student/:course", &catchScreen{}), nil)

	s, params, ok := r.Resolve(RoutePath("/orders/a%2Fb"))
	if !ok || s.Path != "/orders/:id" || params["id"] != "a/b" {
		t.Fatalf("/orders/a%%2Fb resolved to %v %v %v, want /orders/:id id=a/b", s, params, ok)
	}
	_, params, ok = r.Resolve(RoutePath("/enrollments/s%2F1/c%25"))
	if !ok || params["student"] != "s/1" || params["course"] != "c%" {
		t.Fatalf("composite key params = %v (ok=%v), want student=s/1 course=c%%", params, ok)
	}
	if s, _, _ := r.Resolve(RoutePath("/orders/a/b")); s == nil || s.Path != "/orders/a/b" {
		t.Fatalf("/orders/a/b must stay the static screen, got %v", s)
	}
}

func TestMatchPathIsDecoded(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/:id", &catchScreen{}), nil)
	m, ok := r.MatchFor(RoutePath("/orders/a%2Fb"))
	if !ok || m.Path() != "/orders/a/b" || m.Param("id") != "a/b" {
		t.Fatalf("match = %q id=%q (ok=%v)", m.Path(), m.Param("id"), ok)
	}
}

func TestRedirectCarriesSlashKey(t *testing.T) {
	a := NewApp("redir")
	a.Register("/orders/:id", &catchScreen{}, nil)
	a.RedirectPattern("/o/:id", "/orders/:id")
	got, ok := a.ResolveRedirect(RoutePath("/o/a%2Fb"))
	if !ok || got != "/orders/a%2Fb" {
		t.Fatalf("redirect = %q (ok=%v), want /orders/a%%2Fb", got, ok)
	}
}

// A carried "?" or "#" stays in the path: written raw, the browser would
// read the rest of the value as a query or a fragment.
func TestRedirectEscapesQueryChars(t *testing.T) {
	a := NewApp("redir")
	a.Register("/orders/:id", &catchScreen{}, nil)
	a.Register("/files/{rest...}", &catchScreen{}, nil)
	a.RedirectPattern("/o/:id", "/orders/:id")
	a.RedirectPattern("/f/{rest...}", "/files/{rest...}")
	for from, want := range map[string]string{
		"/o/a%3Fb":     "/orders/a%3Fb",
		"/o/a%23b":     "/orders/a%23b",
		"/f/x/a%3Fb":   "/files/x/a%3Fb",
		"/f/x/a%2523b": "/files/x/a%2523b",
	} {
		got, ok := a.ResolveRedirect(RoutePath(from))
		if !ok || got != want {
			t.Errorf("redirect %s = %q (ok=%v), want %s", from, got, ok, want)
		}
	}
}
