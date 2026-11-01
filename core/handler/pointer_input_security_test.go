package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A pointer-typed input binds through one more pointer level: a
// Handler[*T, O] hands Bind a **T, ToolAdapter[*T] hands bindToolParams
// one, and a caller decoding into a *T variable writes
// UnmarshalStrict(data, &p). Bind, validateBodyKeys, isStructPointer and
// bindToolParams each recognised only exactly *struct, so the **T case
// fell to the lax non-struct path: no duplicate, case-folded or unknown
// key refusal, and no query/path/header binding at all. These pin that
// the struct rules hold through every pointer level.

type ptrRoleReq struct {
	Role string `json:"role"`
	Q    string `query:"q" json:"-"`
	ID   string `header:"X-Id" json:"-"`
}

func TestHandlerPointerInput_RefusesSmuggledKeys(t *testing.T) {
	var got string
	h := HandlerAdapter(Handler[*ptrRoleReq, map[string]string](func(_ context.Context, in *ptrRoleReq) (map[string]string, error) {
		if in != nil {
			got = in.Role
		}
		return map[string]string{"ok": "1"}, nil
	}))
	for _, body := range []string{
		`{"role":"member","Role":"admin"}`, // case-folded twin, last wins
		`{"role":"member","role":"admin"}`, // exact duplicate, last wins
		`{"ROLE":"admin"}`,                 // case-folded single key
		`{"role":"x","is_admin":true}`,     // unknown key
	} {
		got = ""
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Handler[*T] body %s: status %d role=%q, want 400 as Handler[T] answers", body, w.Code, got)
		}
	}
}

func TestHandlerPointerInput_BindsQueryAndHeader(t *testing.T) {
	var got *ptrRoleReq
	h := HandlerAdapter(Handler[*ptrRoleReq, map[string]string](func(_ context.Context, in *ptrRoleReq) (map[string]string, error) {
		got = in
		return map[string]string{}, nil
	}))
	r := httptest.NewRequest(http.MethodGet, "/?q=hello", nil)
	r.Header.Set("X-Id", "7")
	h(httptest.NewRecorder(), r)
	if got == nil || got.Q != "hello" || got.ID != "7" {
		t.Fatalf("Handler[*T] GET ?q=hello X-Id:7: handler got %+v, want Q=hello ID=7", got)
	}

	// The body still lands on top of the bound pointer.
	r = httptest.NewRequest(http.MethodPost, "/?q=hello", strings.NewReader(`{"role":"member"}`))
	r.Header.Set("Content-Type", "application/json")
	h(httptest.NewRecorder(), r)
	if got == nil || got.Q != "hello" || got.Role != "member" {
		t.Fatalf("Handler[*T] POST ?q=hello: handler got %+v, want Q=hello Role=member", got)
	}
}

func TestUnmarshalStrict_PointerToPointerRefusesUnknownKey(t *testing.T) {
	var p *ptrRoleReq
	if err := UnmarshalStrict([]byte(`{"role":"x","is_admin":true}`), &p); err == nil {
		t.Fatal("UnmarshalStrict into **T accepted unknown key is_admin; into *T it is refused")
	}
	p = nil
	if err := UnmarshalStrict([]byte(`{"role":"x","Role":"y"}`), &p); err == nil {
		t.Fatal("UnmarshalStrict into **T accepted case-folded twin keys")
	}
	p = nil
	if err := UnmarshalStrict([]byte(`{"role":"x"}`), &p); err != nil || p == nil || p.Role != "x" {
		t.Fatalf("UnmarshalStrict into **T: p=%+v err=%v, want Role=x", p, err)
	}

	// Below the top level only the decoder's DisallowUnknownFields sees an
	// unknown key, and it is switched on for struct destinations alone.
	var n *struct {
		Inner struct {
			A string `json:"a"`
		} `json:"inner"`
	}
	if err := UnmarshalStrict([]byte(`{"inner":{"a":"x","is_admin":true}}`), &n); err == nil {
		t.Fatal("UnmarshalStrict into **T accepted a nested unknown key")
	}
}

// Every pointer level is followed: a **T input binds its tags too.
func TestBindPointerToPointerInput(t *testing.T) {
	var pp **ptrRoleReq
	if err := Bind(httptest.NewRequest(http.MethodGet, "/?q=deep", nil), &pp); err != nil {
		t.Fatal(err)
	}
	if pp == nil || *pp == nil || (*pp).Q != "deep" {
		t.Fatalf("Bind into ***T: got %v, want Q=deep", pp)
	}
}

func TestToolAdapterPointerInput_StrictAndTagged(t *testing.T) {
	var got *ptrRoleReq
	h := Handler[*ptrRoleReq, string](func(_ context.Context, in *ptrRoleReq) (string, error) {
		got = in
		return "", nil
	})
	if _, err := ToolAdapter(h)(context.Background(), map[string]any{"role": "x", "is_admin": true}); err == nil {
		t.Fatal("ToolAdapter[*T] accepted unknown key is_admin; ToolAdapter[T] refuses it")
	}
	if _, err := ToolAdapter(h)(context.Background(), map[string]any{"q": "hi"}); err != nil {
		t.Fatalf("ToolAdapter[*T] query param: %v", err)
	}
	if got == nil || got.Q != "hi" {
		t.Fatalf("ToolAdapter[*T] {q:hi}: handler got %+v, want Q=hi", got)
	}
}
