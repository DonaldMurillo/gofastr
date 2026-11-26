package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSweepSafeRelativePath(t *testing.T) {
	for _, p := range []string{"/", "/a/b?x=1", "/a%20b"} {
		if !IsSafeRelativePath(p) {
			t.Errorf("IsSafeRelativePath(%q) = false", p)
		}
	}
	for _, p := range []string{"", "a", "//evil", "/%2Fevil", "/a\\b", "/a%5Cb", "/a\x00", "/a%0d%0a", "/a\tb", "/a%09b", "http://evil/"} {
		if IsSafeRelativePath(p) {
			t.Errorf("IsSafeRelativePath(%q) = true", p)
		}
	}
}

func TestSweepBindRefusesInfinity(t *testing.T) {
	for _, v := range []string{"Inf", "-Inf", "+Inf", "NaN"} {
		var dst struct {
			F float64 `query:"f"`
		}
		r := httptest.NewRequest(http.MethodGet, "/?f="+url.QueryEscape(v), nil)
		if err := Bind(r, &dst); err == nil {
			t.Errorf("?f=%s bound %v", v, dst.F)
		}
	}
}

func TestSweepBindCutsQueryAtDEL(t *testing.T) {
	var dst struct {
		Q string `query:"q"`
	}
	r := httptest.NewRequest(http.MethodGet, "/?q=ab%7Fcd", nil)
	if err := Bind(r, &dst); err != nil || dst.Q != "ab" {
		t.Fatalf("Bind kept a DEL byte: %q, %v", dst.Q, err)
	}
}

func TestWithQuery(t *testing.T) {
	setSaved := func(q url.Values) { q.Set("saved", "v1") }
	delSaved := func(q url.Values) { q.Del("saved") }
	for _, tc := range []struct {
		name   string
		target string
		edit   func(url.Values)
		want   string
	}{
		{"plain path", "/orders", setSaved, "/orders?saved=v1"},
		{"fragment only", "/orders#list", setSaved, "/orders?saved=v1#list"},
		{"query and fragment", "/orders?sort=name#list", setSaved, "/orders?saved=v1&sort=name#list"},
		{"replaces existing key before fragment", "/orders?saved=old&sort=name#list", setSaved, "/orders?saved=v1&sort=name#list"},
		{"question mark inside fragment", "/orders#list?sort=name", setSaved, "/orders?saved=v1#list?sort=name"},
		{"multiple hashes keep first split", "/orders?a=1#frag#2", setSaved, "/orders?a=1&saved=v1#frag#2"},
		{"empty fragment preserved", "/orders#", setSaved, "/orders?saved=v1#"},
		{"delete last param keeps fragment", "/orders?saved=v1#list", delSaved, "/orders#list"},
		{"delete one param keeps others and fragment", "/orders?saved=v1&sort=name#list", delSaved, "/orders?sort=name#list"},
		{"malformed escape keeps valid params", "/orders?a=1&bad=%ZZ#list", setSaved, "/orders?a=1&saved=v1#list"},
		{"nil edit preserves target", "/orders?a=1#list", nil, "/orders?a=1#list"},
		{"delete from empty query", "/orders", delSaved, "/orders"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithQuery(tc.target, tc.edit); got != tc.want {
				t.Errorf("WithQuery(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}
