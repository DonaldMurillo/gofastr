package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An empty query value is the parameter being absent, for every kind
// (H9, 2026-11): `?page=` used to fail an int field with a 400 while
// `?q=` bound "" into a string, so one empty form field read two ways.
// The `,empty` tag option opts a field back in to an explicit empty.

type emptyDefaults struct {
	Q    string  `query:"q"`
	Page int     `query:"page"`
	Flag bool    `query:"flag"`
	P    *string `query:"p"`
	H    string  `header:"X-H"`
}

type emptyOptIn struct {
	Q  string  `query:"q,empty"`
	P  *string `query:"p,empty"`
	N  int     `query:"n,empty"`
	NP *int    `query:"np,empty"`
	H  *string `header:"X-H,empty"`
	HS string  `header:"X-HS,empty"`
}

func TestBindEmptyQueryIsAbsent(t *testing.T) {
	kept := func() emptyDefaults { return emptyDefaults{Q: "kept", Page: 3, Flag: true, H: "kept"} }
	// One kind per request, so each kind's rule is proven on its own.
	for _, query := range []string{"q=", "page=", "flag=", "p=", ""} {
		in := kept()
		req := httptest.NewRequest(http.MethodGet, "/s?"+query, nil)
		if query == "" {
			req.Header["X-H"] = []string{""}
		}
		if err := Bind(req, &in); err != nil {
			t.Errorf("%q: an empty value was not treated as absent: %v", query, err)
			continue
		}
		if in != kept() {
			t.Errorf("%q (or an empty header): an empty value overwrote a field: %+v", query, in)
		}
	}
	// A control byte the sanitizer cuts down to nothing is empty too.
	in := emptyDefaults{}
	if err := Bind(httptest.NewRequest(http.MethodGet, "/s?p=%00x", nil), &in); err != nil || in.P != nil {
		t.Fatalf("a value cut to empty bound: %+v %v", in, err)
	}
}

func TestBindEmptyOptionBindsExplicitEmpty(t *testing.T) {
	var in emptyOptIn
	req := httptest.NewRequest(http.MethodGet, "/s?q=&p=&n=&np=", nil)
	req.Header["X-H"] = []string{""}
	req.Header["X-Hs"] = []string{""}
	in.Q, in.N, in.HS = "stale", 9, "stale"
	if err := Bind(req, &in); err != nil {
		t.Fatal(err)
	}
	if in.Q != "" || in.N != 0 || in.HS != "" {
		t.Fatalf(",empty did not bind the zero value: %+v", in)
	}
	if in.P == nil || *in.P != "" {
		t.Fatalf(",empty *string: want pointer to \"\", got %v", in.P)
	}
	if in.NP == nil || *in.NP != 0 {
		t.Fatalf(",empty *int: want pointer to 0, got %v", in.NP)
	}
	if in.H == nil || *in.H != "" {
		t.Fatalf(",empty header *string: want pointer to \"\", got %v", in.H)
	}

	// Absent stays absent: the pointer is nil, so a handler can tell
	// ?p= from no p at all.
	var absent emptyOptIn
	if err := Bind(httptest.NewRequest(http.MethodGet, "/s", nil), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.P != nil || absent.NP != nil || absent.H != nil {
		t.Fatalf("an absent ,empty param allocated: %+v", absent)
	}

	// A present value still binds normally under ,empty.
	var full emptyOptIn
	if err := Bind(httptest.NewRequest(http.MethodGet, "/s?q=go&n=4&p=x", nil), &full); err != nil {
		t.Fatal(err)
	}
	if full.Q != "go" || full.N != 4 || full.P == nil || *full.P != "x" {
		t.Fatalf("got %+v", full)
	}
}

func TestBindTagOptionMistakesAreRefused(t *testing.T) {
	type typo struct {
		Q string `query:"q,emtpy"`
	}
	type onPath struct {
		ID string `path:"id,empty"`
	}
	type noName struct {
		Q string `query:",empty"`
	}
	for _, tc := range []struct {
		name string
		dst  any
	}{{"typo", &typo{}}, {"path", &onPath{}}, {"noname", &noName{}}} {
		name := tc.name
		err := Bind(httptest.NewRequest(http.MethodGet, "/s", nil), tc.dst)
		if err == nil {
			t.Errorf("%s: a bad tag option bound silently", name)
			continue
		}
		var he *Error
		if errors.As(err, &he) {
			t.Errorf("%s: a tag mistake is the server's, not a 4xx: %v", name, err)
		}
		if !strings.HasPrefix(err.Error(), "bind: ") {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}

func TestToolAdapterEmptyParam(t *testing.T) {
	echoD := Handler[emptyDefaults, emptyDefaults](func(_ context.Context, in emptyDefaults) (emptyDefaults, error) { return in, nil })
	out, err := ToolAdapter(echoD)(context.Background(), map[string]any{"q": "", "page": "", "p": ""})
	if err != nil {
		t.Fatalf("an empty tool param was not absent: %v", err)
	}
	if got := out.(emptyDefaults); got.Q != "" || got.Page != 0 || got.P != nil {
		t.Fatalf("got %+v", got)
	}

	echoO := Handler[emptyOptIn, emptyOptIn](func(_ context.Context, in emptyOptIn) (emptyOptIn, error) { return in, nil })
	out, err = ToolAdapter(echoO)(context.Background(), map[string]any{"p": "", "np": ""})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(emptyOptIn)
	if got.P == nil || *got.P != "" || got.NP == nil || *got.NP != 0 {
		t.Fatalf(",empty tool params: got %+v", got)
	}
}

// A pointer-typed input (Handler[*T, O]) stays nil when the only tagged
// value is empty: absence is decided before settableField allocates the
// pointer, so `?q=` is the same as no query at all.
func TestBindEmptyValueAllocatesNoPointerInput(t *testing.T) {
	var in *emptyDefaults
	if err := Bind(httptest.NewRequest(http.MethodGet, "/s?q=&page=", nil), &in); err != nil {
		t.Fatal(err)
	}
	if in != nil {
		t.Fatalf("an empty query value allocated the input: %+v", *in)
	}
	req := httptest.NewRequest(http.MethodGet, "/s", nil)
	req.Header["X-H"] = []string{""}
	if err := Bind(req, &in); err != nil || in != nil {
		t.Fatalf("an empty header allocated the input: %v %v", in, err)
	}
	var opt *emptyOptIn
	if err := Bind(httptest.NewRequest(http.MethodGet, "/s?p=", nil), &opt); err != nil {
		t.Fatal(err)
	}
	if opt == nil || opt.P == nil || *opt.P != "" {
		t.Fatalf(",empty on a pointer input: got %+v", opt)
	}

	echo := Handler[*emptyDefaults, *emptyDefaults](func(_ context.Context, in *emptyDefaults) (*emptyDefaults, error) { return in, nil })
	out, err := ToolAdapter(echo)(context.Background(), map[string]any{"q": ""})
	if err != nil || out.(*emptyDefaults) != nil {
		t.Fatalf("an empty tool param allocated the input: %v %v", out, err)
	}
}
