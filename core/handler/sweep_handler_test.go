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
